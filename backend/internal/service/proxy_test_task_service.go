package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const (
	proxyTaskRunTimeout       = 5 * time.Minute
	proxyTaskProbeConcurrency = 4
	proxyTaskEmailTimeout     = 30 * time.Second
	proxyTaskCleanupMargin    = 10 * time.Second
)

type ProxyTestTaskService struct {
	repo   ProxyTestTaskRepository
	tester ProxyTaskTester
	mailer ProxyTaskMailer
	now    func() time.Time
}

func NewProxyTestTaskService(repo ProxyTestTaskRepository, tester ProxyTaskTester, mailer ProxyTaskMailer) *ProxyTestTaskService {
	return &ProxyTestTaskService{repo: repo, tester: tester, mailer: mailer, now: time.Now}
}

func (s *ProxyTestTaskService) smtpConfigured(ctx context.Context) bool {
	if s.mailer == nil {
		return false
	}
	cfg, err := s.mailer.GetSMTPConfig(ctx)
	return err == nil && cfg != nil && strings.TrimSpace(cfg.Host) != "" && strings.TrimSpace(cfg.From) != ""
}

func (s *ProxyTestTaskService) List(ctx context.Context) ([]*ProxyTestTask, error) {
	tasks, err := s.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	configured := s.smtpConfigured(ctx)
	for _, task := range tasks {
		task.SMTPConfigured = configured
	}
	return tasks, nil
}

func (s *ProxyTestTaskService) Get(ctx context.Context, id int64) (*ProxyTestTask, error) {
	task, err := s.repo.Get(ctx, id)
	if err == nil {
		task.SMTPConfigured = s.smtpConfigured(ctx)
	}
	return task, err
}

func (s *ProxyTestTaskService) Create(ctx context.Context, input *ProxyTestTaskInput) (*ProxyTestTask, error) {
	return s.save(ctx, 0, input)
}

func (s *ProxyTestTaskService) Update(ctx context.Context, id int64, input *ProxyTestTaskInput) (*ProxyTestTask, error) {
	return s.save(ctx, id, input)
}

func (s *ProxyTestTaskService) save(ctx context.Context, id int64, input *ProxyTestTaskInput) (*ProxyTestTask, error) {
	var out *ProxyTestTask
	err := s.repo.WithTaskLock(ctx, id, true, func(locked context.Context) error {
		task := &ProxyTestTask{Timezone: "Asia/Shanghai", CronExpression: "*/5 * * * *", Enabled: true, FailureThreshold: 1, TimeoutSeconds: 10}
		if id != 0 {
			var err error
			task, err = s.repo.Get(locked, id)
			if err != nil {
				return err
			}
		}
		mergeProxyTaskInput(task, input)
		if err := validateProxyTask(task, s.now()); err != nil {
			return err
		}
		task.SMTPConfigured = s.smtpConfigured(locked)
		if task.Enabled && !task.SMTPConfigured {
			return ErrEmailNotConfigured
		}
		var err error
		out, err = s.repo.Save(locked, task)
		if out != nil {
			out.SMTPConfigured = task.SMTPConfigured
		}
		return err
	})
	return out, err
}

func mergeProxyTaskInput(task *ProxyTestTask, input *ProxyTestTaskInput) {
	if input == nil {
		return
	}
	if input.Name != nil {
		task.Name = strings.TrimSpace(*input.Name)
	}
	if input.ProxyIDs != nil {
		task.ProxyIDs = append([]int64{}, input.ProxyIDs...)
	}
	if input.CronExpression != nil {
		task.CronExpression = strings.TrimSpace(*input.CronExpression)
	}
	if input.Timezone != nil {
		task.Timezone = strings.TrimSpace(*input.Timezone)
	}
	if input.Enabled != nil {
		task.Enabled = *input.Enabled
	}
	if input.Recipients != nil {
		task.Recipients = append([]string{}, input.Recipients...)
	}
	if input.FailureThreshold != nil {
		task.FailureThreshold = *input.FailureThreshold
	}
	if input.TimeoutSeconds != nil {
		task.TimeoutSeconds = *input.TimeoutSeconds
	}
}

func validateProxyTask(task *ProxyTestTask, now time.Time) error {
	bad := func(message string) error { return infraerrors.BadRequest("PROXY_TEST_TASK_INVALID", message) }
	if task.Name == "" || utf8.RuneCountInString(task.Name) > 100 || strings.ContainsAny(task.Name, "\r\n") {
		return bad("task name must contain 1 to 100 characters without newlines")
	}
	if len(task.ProxyIDs) == 0 || len(task.ProxyIDs) > 20 {
		return bad("select between 1 and 20 proxies")
	}
	seenIDs := make(map[int64]bool, len(task.ProxyIDs))
	for _, id := range task.ProxyIDs {
		if id <= 0 || seenIDs[id] {
			return bad("proxy ids must be positive and unique")
		}
		seenIDs[id] = true
	}
	sort.Slice(task.ProxyIDs, func(i, j int) bool { return task.ProxyIDs[i] < task.ProxyIDs[j] })
	if task.FailureThreshold < 1 || task.FailureThreshold > 20 {
		return bad("failure threshold must be between 1 and 20")
	}
	if task.TimeoutSeconds < 3 || task.TimeoutSeconds > 30 {
		return bad("probe timeout must be between 3 and 30 seconds")
	}
	if len(task.Recipients) == 0 || len(task.Recipients) > 10 {
		return bad("specify between 1 and 10 notification recipients")
	}
	seenEmails := make(map[string]bool, len(task.Recipients))
	recipients := make([]string, 0, len(task.Recipients))
	for _, raw := range task.Recipients {
		address := strings.TrimSpace(raw)
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n") || len(address) > 254 {
			return bad("notification recipients must be plain valid email addresses")
		}
		key := strings.ToLower(address)
		if !seenEmails[key] {
			recipients = append(recipients, address)
			seenEmails[key] = true
		}
	}
	task.Recipients = recipients
	if task.Enabled {
		if err := validateProxyTaskRunBudget(task); err != nil {
			return err
		}
	}
	next, err := nextProxyTaskRun(task.CronExpression, task.Timezone, now)
	if err != nil {
		return bad(err.Error())
	}
	task.NextRunAt = nil
	if task.Enabled {
		task.NextRunAt = &next
	}
	return nil
}

// Reserve both independent probes, two controller snapshots, every recipient's
// SMTP deadline, and final persistence time. This prevents the last probe batch
// from being canceled on every run when an enabled task exceeds its total budget.
func validateProxyTaskRunBudget(task *ProxyTestTask) error {
	batches := (len(task.ProxyIDs) + proxyTaskProbeConcurrency - 1) / proxyTaskProbeConcurrency
	probeBudget := time.Duration(batches) * (2*time.Duration(task.TimeoutSeconds)*time.Second + 4*time.Second)
	budget := probeBudget + time.Duration(len(task.Recipients))*proxyTaskEmailTimeout + proxyTaskCleanupMargin
	if budget > proxyTaskRunTimeout {
		return infraerrors.BadRequest("PROXY_TEST_TASK_BUDGET_EXCEEDED", "task exceeds the 5-minute execution budget; reduce the number of proxies or email recipients, or lower the probe timeout")
	}
	return nil
}

func nextProxyTaskRun(expression, timezone string, from time.Time) (time.Time, error) {
	if len(expression) > 100 || len(strings.Fields(expression)) != 5 {
		return time.Time{}, fmt.Errorf("cron expression must have exactly five fields")
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timezone")
	}
	schedule, err := scheduledTestCronParser.Parse(expression)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid cron expression: %w", err)
	}
	next := schedule.Next(from.In(loc))
	if next.IsZero() {
		return time.Time{}, fmt.Errorf("cron expression has no future execution")
	}
	return next, nil
}

func (s *ProxyTestTaskService) Delete(ctx context.Context, id int64) error {
	return s.repo.WithTaskLock(ctx, id, true, func(locked context.Context) error {
		if _, err := s.repo.Get(locked, id); err != nil {
			return err
		}
		return s.repo.Delete(locked, id)
	})
}

// Run refreshes diagnostics without advancing or clearing the scheduled alert cycle.
func (s *ProxyTestTaskService) Run(ctx context.Context, id int64) (*ProxyTestTask, error) {
	return s.run(ctx, id, false)
}

func (s *ProxyTestTaskService) run(ctx context.Context, id int64, scheduled bool) (*ProxyTestTask, error) {
	runCtx, cancel := context.WithTimeout(ctx, proxyTaskRunTimeout)
	defer cancel()
	var out *ProxyTestTask
	err := s.repo.WithTaskLock(runCtx, id, false, func(locked context.Context) (runErr error) {
		task, err := s.repo.Get(locked, id)
		if err != nil {
			return err
		}
		out = task
		if scheduled && (!task.Enabled || task.NextRunAt == nil || task.NextRunAt.After(s.now())) {
			return nil // A different Pod may have completed this due run while we waited.
		}
		if err := validateProxyTaskRunBudget(task); err != nil {
			return err
		}
		started := s.now()
		task.LastRunAt, task.LastRunStatus, task.LastError = &started, "running", ""
		defer func() {
			if runErr != nil {
				task.LastRunStatus, task.LastError = "error", sanitizeProxyTaskError(runErr.Error())
			}
			if locked.Err() != nil {
				task.LastRunStatus, task.LastError = "error", "Task execution timed out or was canceled"
			}
			if scheduled {
				if next, err := nextProxyTaskRun(task.CronExpression, task.Timezone, s.now()); err == nil {
					task.NextRunAt = &next
				}
			}
			// A canceled request must still finalize the result while the lock session is held.
			cleanup, cleanupCancel := context.WithTimeout(context.WithoutCancel(locked), 5*time.Second)
			defer cleanupCancel()
			runErr = errors.Join(runErr, s.repo.SaveRun(cleanup, task, scheduled))
		}()
		if err := s.repo.SaveRun(locked, task, false); err != nil {
			return err
		}
		s.probeTargets(locked, task, scheduled)
		task.SMTPConfigured = s.smtpConfigured(locked)
		task.LastRunStatus = "success"
		if !scheduled {
			task.LastRunStatus = "manual"
		}
		for _, target := range task.Targets {
			if target.LastNodeStatus == "unknown" {
				task.LastRunStatus = "error"
				task.LastError = "Original-node diagnostics incomplete: " + sanitizeProxyTaskError(target.LastError)
				break
			}
		}
		if scheduled {
			next, err := nextProxyTaskRun(task.CronExpression, task.Timezone, s.now())
			if err != nil {
				task.LastError, task.LastRunStatus = err.Error(), "error"
			} else {
				task.NextRunAt = &next
			}
			// Persist fault counts before attempting delivery. A failed write must not send untracked alerts.
			if err := s.repo.SaveRun(locked, task, true); err != nil {
				return err
			}
			if !task.SMTPConfigured {
				task.LastError, task.LastRunStatus = "Email service is not configured", "error"
			} else if err := s.notifyFailures(locked, task); err != nil {
				task.LastError, task.LastRunStatus = sanitizeProxyTaskError(err.Error()), "error"
			}
		}
		return nil
	})
	return out, err
}

func (s *ProxyTestTaskService) probeTargets(ctx context.Context, task *ProxyTestTask, scheduled bool) {
	sem := make(chan struct{}, proxyTaskProbeConcurrency)
	var wg sync.WaitGroup
	for _, target := range task.Targets {
		wg.Add(1)
		go func(target *ProxyTestTaskTarget) {
			defer wg.Done()
			var result *ProxyTestResult
			var err error
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				timeout := time.Duration(task.TimeoutSeconds) * time.Second
				probeCtx, cancel := context.WithTimeout(ctx, 3*timeout+5*time.Second)
				probeCtx = WithProxyProbeTimeout(probeCtx, timeout)
				if s.tester == nil {
					err = fmt.Errorf("proxy test service is unavailable")
				} else {
					result, err = s.tester.TestProxy(probeCtx, target.ProxyID)
				}
				cancel()
			case <-ctx.Done():
				err = ctx.Err()
			}
			checked := s.now()
			target.LastCheckedAt, target.LastResult = &checked, result
			target.LastNodeStatus, target.LastError = "unknown", ""
			if err != nil {
				target.LastError = sanitizeProxyTaskError(err.Error())
			} else if result != nil {
				target.LastNodeStatus, target.LastError = result.NodeStatus, sanitizeProxyTaskError(result.Message)
				if result.NodeCheckedAt != nil {
					target.LastCheckedAt = result.NodeCheckedAt
				}
				if target.LastNodeStatus == "" { // Compatibility with ordinary proxy implementations.
					target.LastNodeStatus = "unhealthy"
					if result.Success {
						target.LastNodeStatus = "healthy"
					}
				}
				if target.LastNodeStatus == "healthy" {
					target.LastError = ""
				}
			}
			if scheduled {
				if result != nil && result.ProbeConfigVersion != "" {
					// A prior proxy edit can have committed even when its reset write failed.
					// Compare identity digests, not cosmetic edit timestamps; manual runs never overwrite this baseline.
					if oldID, newID := proxyTaskConfigIdentity(target.LastProbeConfigVersion), proxyTaskConfigIdentity(result.ProbeConfigVersion); oldID != "" && oldID != newID {
						target.ConsecutiveFailures, target.FailureStartedAt, target.NotifiedRecipients = 0, nil, []string{}
					}
					target.LastProbeConfigVersion = result.ProbeConfigVersion
				}
				advanceProxyTaskFault(target, task.FailureThreshold, checked)
			}
		}(target)
	}
	wg.Wait()
}

func proxyTaskConfigIdentity(version string) string {
	if _, digest, ok := strings.Cut(version, "-"); ok {
		return digest
	}
	return version
}

func advanceProxyTaskFault(target *ProxyTestTaskTarget, threshold int, now time.Time) {
	switch target.LastNodeStatus {
	case "healthy":
		target.ConsecutiveFailures = 0
		target.FailureStartedAt = nil
		target.NotifiedRecipients = []string{}
	case "unhealthy":
		if target.ConsecutiveFailures == 0 {
			target.FailureStartedAt = &now
		}
		// Saturate at the threshold: no overflow during a long-lived outage.
		if target.ConsecutiveFailures < threshold {
			target.ConsecutiveFailures++
		}
	default:
		target.LastNodeStatus = "unknown"
		if target.ConsecutiveFailures < threshold {
			target.ConsecutiveFailures = 0
			target.FailureStartedAt = nil
		}
	}
}

func (s *ProxyTestTaskService) notifyFailures(ctx context.Context, task *ProxyTestTask) error {
	failures := []string{}
	for _, recipient := range task.Recipients {
		pending := []*ProxyTestTaskTarget{}
		for _, target := range task.Targets {
			if target.LastNodeStatus == "unhealthy" && target.ConsecutiveFailures >= task.FailureThreshold && !containsProxyTaskRecipient(target.NotifiedRecipients, recipient) {
				pending = append(pending, target)
			}
		}
		if len(pending) == 0 {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		emailCtx, cancel := context.WithTimeout(ctx, proxyTaskEmailTimeout)
		subject := "[Sub2API] 原节点检测失败：" + task.Name
		var err error
		if mailer, ok := s.mailer.(interface {
			SendEmailContext(context.Context, string, string, string) error
		}); ok {
			err = mailer.SendEmailContext(emailCtx, recipient, subject, proxyTaskEmailBody(task, pending))
		} else {
			err = s.mailer.SendEmail(emailCtx, recipient, subject, proxyTaskEmailBody(task, pending))
		}
		cancel()
		if err != nil {
			failures = append(failures, recipient+": "+sanitizeProxyTaskError(err.Error()))
			continue
		}
		for _, target := range pending {
			target.NotifiedRecipients = append(target.NotifiedRecipients, recipient)
		}
		// Preserve successful recipients before moving on to another potentially failing delivery.
		if err := s.repo.SaveRun(ctx, task, true); err != nil {
			return err
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("notification delivery failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func containsProxyTaskRecipient(recipients []string, wanted string) bool {
	for _, recipient := range recipients {
		if strings.EqualFold(recipient, wanted) {
			return true
		}
	}
	return false
}

func proxyTaskEmailBody(task *ProxyTestTask, targets []*ProxyTestTaskTarget) string {
	var body strings.Builder
	body.WriteString("<h2>代理原节点检测失败</h2><p>任务：" + html.EscapeString(task.Name) + "</p>")
	loc, err := time.LoadLocation(task.Timezone)
	if err != nil {
		loc = time.UTC
	}
	for _, target := range targets {
		name := target.ProxyName
		if target.MihomoPrimaryNode != "" {
			name += "（" + target.MihomoPrimaryNode + "）"
		}
		body.WriteString("<h3>" + html.EscapeString(name) + "</h3><ul>")
		body.WriteString(fmt.Sprintf("<li>连续失败：%d 次</li>", target.ConsecutiveFailures))
		body.WriteString("<li>原因：" + html.EscapeString(sanitizeProxyTaskError(target.LastError)) + "</li>")
		if target.LastCheckedAt != nil {
			body.WriteString("<li>检测时间：" + html.EscapeString(target.LastCheckedAt.In(loc).Format("2006-01-02 15:04:05 MST")) + "</li>")
		}
		if result := target.LastResult; result != nil {
			route := result.PreferredNode
			if route == "" {
				route = "未知"
			} else if result.RouteFixed {
				route += "（手动指定）"
			} else if result.IsBackup {
				route += "（备用）"
			}
			body.WriteString("<li>最近同轮新连接首选：" + html.EscapeString(route) + "</li>")
			body.WriteString("<li>最近同轮业务入口：" + html.EscapeString(result.BusinessStatus) + "</li>")
			body.WriteString("<li>执行实例：" + html.EscapeString(result.SampledPod) + "</li>")
		}
		body.WriteString("</ul>")
	}
	body.WriteString("<p>原节点经指定目标探测失败。请检查原节点，必要时自行更换代理配置并重启服务。</p><p>以上为本次探测结果，不表示 GPT 账户不可用，也不表示全部账户或存量连接已迁移。</p>")
	return body.String()
}

var proxyTaskURLPattern = regexp.MustCompile(`(?i)(?:https?|socks5h?)://[^\s<>"']+`)

func sanitizeProxyTaskError(message string) string {
	message = proxyTaskURLPattern.ReplaceAllStringFunc(message, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return "[proxy URL]"
		}
		parsed.User = nil
		return parsed.String()
	})
	if len(message) > 2000 {
		message = message[:2000]
	}
	return message
}

func (s *ProxyTestTaskService) SendTestEmail(ctx context.Context, id int64) error {
	ctx, cancel := context.WithTimeout(ctx, proxyTaskRunTimeout)
	defer cancel()
	return s.repo.WithTaskLock(ctx, id, false, func(locked context.Context) error {
		task, err := s.repo.Get(locked, id)
		if err != nil {
			return err
		}
		if !s.smtpConfigured(locked) {
			return ErrEmailNotConfigured
		}
		for _, recipient := range task.Recipients {
			emailCtx, cancel := context.WithTimeout(locked, proxyTaskEmailTimeout)
			subject, body := "[Sub2API] 代理定时检测测试邮件", "<p>任务："+html.EscapeString(task.Name)+"</p><p>这是管理员主动发送的测试邮件，不改变正式故障通知周期。</p>"
			var err error
			if mailer, ok := s.mailer.(interface {
				SendEmailContext(context.Context, string, string, string) error
			}); ok {
				err = mailer.SendEmailContext(emailCtx, recipient, subject, body)
			} else {
				err = s.mailer.SendEmail(emailCtx, recipient, subject, body)
			}
			cancel()
			if err != nil {
				return err
			}
		}
		return nil
	})
}
