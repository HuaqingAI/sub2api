package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type proxyTaskMemoryRepo struct {
	mu         sync.Mutex
	task       *ProxyTestTask
	saveCalls  int
	failSaveAt int
	locked     bool
}

func cloneTask(task *ProxyTestTask) *ProxyTestTask {
	raw, _ := json.Marshal(task)
	var out ProxyTestTask
	_ = json.Unmarshal(raw, &out)
	return &out
}
func (r *proxyTaskMemoryRepo) List(context.Context) ([]*ProxyTestTask, error) {
	return []*ProxyTestTask{cloneTask(r.task)}, nil
}
func (r *proxyTaskMemoryRepo) Get(context.Context, int64) (*ProxyTestTask, error) {
	return cloneTask(r.task), nil
}
func (r *proxyTaskMemoryRepo) ListDue(context.Context, time.Time) ([]int64, error) {
	return []int64{r.task.ID}, nil
}
func (r *proxyTaskMemoryRepo) Save(_ context.Context, task *ProxyTestTask) (*ProxyTestTask, error) {
	r.task = cloneTask(task)
	return cloneTask(r.task), nil
}
func (r *proxyTaskMemoryRepo) Delete(context.Context, int64) error { return nil }
func (r *proxyTaskMemoryRepo) SaveRun(ctx context.Context, task *ProxyTestTask, scheduled bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.saveCalls++
	if r.saveCalls == r.failSaveAt {
		return errors.New("database write failed")
	}
	next := cloneTask(task)
	if !scheduled {
		next.NextRunAt = r.task.NextRunAt
		for i, target := range next.Targets {
			target.ConsecutiveFailures = r.task.Targets[i].ConsecutiveFailures
			target.FailureStartedAt = r.task.Targets[i].FailureStartedAt
			target.NotifiedRecipients = append([]string{}, r.task.Targets[i].NotifiedRecipients...)
			target.LastProbeConfigVersion = r.task.Targets[i].LastProbeConfigVersion
		}
	}
	r.task = next
	return nil
}
func (r *proxyTaskMemoryRepo) WithTaskLock(ctx context.Context, _ int64, _ bool, fn func(context.Context) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.locked = true
	defer func() { r.locked = false }()
	return fn(ctx)
}
func (r *proxyTaskMemoryRepo) WithProxyConfigLock(ctx context.Context, _ int64, fn func(context.Context) error) error {
	return r.WithTaskLock(ctx, 0, true, fn)
}
func (r *proxyTaskMemoryRepo) ResetProxyTargets(context.Context, int64) error { return nil }

type proxyTaskTestFunc func(context.Context, int64) (*ProxyTestResult, error)

func (f proxyTaskTestFunc) TestProxy(ctx context.Context, id int64) (*ProxyTestResult, error) {
	return f(ctx, id)
}

type proxyTaskFakeMailer struct {
	configured bool
	sends      []string
	bodies     []string
	fail       map[string]bool
}

func (m *proxyTaskFakeMailer) GetSMTPConfig(context.Context) (*SMTPConfig, error) {
	if !m.configured {
		return nil, ErrEmailNotConfigured
	}
	return &SMTPConfig{Host: "smtp.test", From: "sender@test.example"}, nil
}
func (m *proxyTaskFakeMailer) SendEmail(_ context.Context, to, _, body string) error {
	m.sends = append(m.sends, to)
	m.bodies = append(m.bodies, body)
	if m.fail[to] {
		return errors.New("temporary SMTP error")
	}
	return nil
}

func proxyTaskFixture() (*proxyTaskMemoryRepo, *proxyTaskFakeMailer, time.Time) {
	now := time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	repo := &proxyTaskMemoryRepo{task: &ProxyTestTask{
		ID: 1, Name: "美国节点", ProxyIDs: []int64{10}, CronExpression: "*/5 * * * *", Timezone: "Asia/Shanghai", Enabled: true,
		Recipients: []string{"a@test.example", "b@test.example"}, FailureThreshold: 1, TimeoutSeconds: 10, NextRunAt: &due,
		ConfigVersion: 1, Targets: []*ProxyTestTaskTarget{{TaskID: 1, ProxyID: 10, ProxyName: "us1", MihomoPrimaryNode: "us1", LastNodeStatus: "unknown", NotifiedRecipients: []string{}}},
	}}
	mailer := &proxyTaskFakeMailer{configured: true, fail: map[string]bool{}}
	return repo, mailer, now
}

func TestProxyTaskFaultCycleAndRecipientRetry(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	status := "unhealthy"
	service := NewProxyTestTaskService(repo, proxyTaskTestFunc(func(context.Context, int64) (*ProxyTestResult, error) {
		return &ProxyTestResult{NodeStatus: status, Success: status == "healthy", BusinessStatus: "healthy", PreferredNode: "us2", IsBackup: true,
			Message: "dial socks5://secret-user:secret-pass@127.0.0.1:17903: timeout"}, nil
	}), mailer)
	service.now = func() time.Time { return now }
	runDue := func() {
		t.Helper()
		due := now.Add(-time.Second)
		repo.task.NextRunAt = &due
		if _, err := service.run(context.Background(), 1, true); err != nil {
			t.Fatal(err)
		}
	}
	mailer.fail["b@test.example"] = true
	runDue()
	if got := repo.task.Targets[0].NotifiedRecipients; !reflect.DeepEqual(got, []string{"a@test.example"}) {
		t.Fatalf("successful recipient lost: %v", got)
	}
	if !strings.Contains(mailer.bodies[0], "us2（备用）") || strings.Contains(mailer.bodies[0], "secret-pass") {
		t.Fatalf("email must describe fallback and redact credentials: %s", mailer.bodies[0])
	}
	mailer.fail["b@test.example"] = false
	runDue()
	runDue()
	if !reflect.DeepEqual(mailer.sends, []string{"a@test.example", "b@test.example", "b@test.example"}) {
		t.Fatalf("only the failed recipient should retry: %v", mailer.sends)
	}
	status = "healthy"
	if _, err := service.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if repo.task.Targets[0].ConsecutiveFailures != 1 || len(repo.task.Targets[0].NotifiedRecipients) != 2 {
		t.Fatal("manual success must not clear the scheduled fault cycle")
	}
	runDue()
	if repo.task.Targets[0].ConsecutiveFailures != 0 || len(repo.task.Targets[0].NotifiedRecipients) != 0 {
		t.Fatal("scheduled recovery must clear the fault cycle")
	}
	status = "unhealthy"
	runDue()
	if len(mailer.sends) != 5 {
		t.Fatalf("a new fault cycle must notify both recipients, got %v", mailer.sends)
	}
}

func TestProxyTaskUnknownInterruptsUnconfirmedButKeepsConfirmedCycle(t *testing.T) {
	now := time.Now()
	target := &ProxyTestTaskTarget{LastNodeStatus: "unhealthy"}
	advanceProxyTaskFault(target, 2, now)
	target.LastNodeStatus = "unknown"
	advanceProxyTaskFault(target, 2, now)
	if target.ConsecutiveFailures != 0 || target.FailureStartedAt != nil {
		t.Fatal("unknown must break an unconfirmed consecutive failure sequence")
	}
	target.LastNodeStatus = "unhealthy"
	advanceProxyTaskFault(target, 2, now)
	advanceProxyTaskFault(target, 2, now)
	target.NotifiedRecipients = []string{"a@test.example"}
	target.LastNodeStatus = "unknown"
	advanceProxyTaskFault(target, 2, now)
	if target.ConsecutiveFailures != 2 || len(target.NotifiedRecipients) != 1 {
		t.Fatal("unknown must retain a confirmed fault cycle without sending again")
	}
}

func TestProxyTaskCronUsesSpecifiedTimezone(t *testing.T) {
	from := time.Date(2026, 11, 17, 1, 10, 0, 0, time.UTC)
	next, err := nextProxyTaskRun("0 8 * * *", "Asia/Shanghai", from)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 11, 18, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("got %s; want %s", next, want)
	}
	if _, err := nextProxyTaskRun("CRON_TZ=UTC 0 8 * * *", "Asia/Shanghai", from); err == nil {
		t.Fatal("embedded timezone directives must not override the form timezone")
	}
}

func TestProxyTaskValidationRejectsMailInjectionAndDuplicateTargets(t *testing.T) {
	repo, _, now := proxyTaskFixture()
	for _, change := range []func(*ProxyTestTask){
		func(task *ProxyTestTask) { task.Recipients = []string{"a@test.example\r\nBcc: secret@test.example"} },
		func(task *ProxyTestTask) { task.ProxyIDs = []int64{10, 10} },
		func(task *ProxyTestTask) { task.FailureThreshold = 0 },
		func(task *ProxyTestTask) { task.TimeoutSeconds = 31 },
	} {
		task := cloneTask(repo.task)
		change(task)
		if err := validateProxyTask(task, now); err == nil {
			t.Fatal("invalid task accepted")
		}
	}
}

func TestProxyTaskValidationEnforcesCombinedExecutionBudget(t *testing.T) {
	repo, _, now := proxyTaskFixture()
	for _, test := range []struct {
		name       string
		proxies    int
		timeout    int
		recipients int
		wantError  bool
	}{
		{name: "default five nodes", proxies: 5, timeout: 10, recipients: 1},
		{name: "twenty nodes exceed five minutes", proxies: 20, timeout: 30, recipients: 1, wantError: true},
		{name: "legal larger batch", proxies: 20, timeout: 20, recipients: 2},
		{name: "exact five-minute boundary", proxies: 4, timeout: 8, recipients: 9},
		{name: "two seconds above boundary", proxies: 4, timeout: 9, recipients: 9, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			task := cloneTask(repo.task)
			task.TimeoutSeconds = test.timeout
			task.ProxyIDs = make([]int64, test.proxies)
			for i := range task.ProxyIDs {
				task.ProxyIDs[i] = int64(i + 1)
			}
			task.Recipients = make([]string, test.recipients)
			for i := range task.Recipients {
				task.Recipients[i] = fmt.Sprintf("admin%d@test.example", i)
			}
			err := validateProxyTask(task, now)
			if (err != nil) != test.wantError {
				t.Fatalf("validation error=%v; wantError=%v", err, test.wantError)
			}
			if test.wantError && !strings.Contains(err.Error(), "reduce the number of proxies") {
				t.Fatalf("budget rejection must explain how to fix the task: %v", err)
			}
		})
	}
}

func TestProxyTaskDisabledOverBudgetDraftCannotRun(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	task := repo.task
	task.Enabled, task.TimeoutSeconds = false, 30
	task.ProxyIDs = make([]int64, 20)
	for i := range task.ProxyIDs {
		task.ProxyIDs[i] = int64(i + 1)
	}
	if err := validateProxyTask(task, now); err != nil {
		t.Fatalf("disabled over-budget draft should be saved: %v", err)
	}
	service := NewProxyTestTaskService(repo, proxyTaskTestFunc(func(context.Context, int64) (*ProxyTestResult, error) {
		t.Fatal("over-budget manual run must be rejected before probing")
		return nil, nil
	}), mailer)
	if _, err := service.Run(context.Background(), task.ID); err == nil {
		t.Fatal("over-budget disabled draft must not start a partial manual run")
	}
	if repo.saveCalls != 0 || len(mailer.sends) != 0 {
		t.Fatal("budget rejection must not modify execution or notification state")
	}
}

func TestProxyTaskConcurrentDueRunsOnlyProbeOnce(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	var probes atomic.Int32
	service := NewProxyTestTaskService(repo, proxyTaskTestFunc(func(context.Context, int64) (*ProxyTestResult, error) {
		probes.Add(1)
		return &ProxyTestResult{NodeStatus: "unhealthy"}, nil
	}), mailer)
	service.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := service.run(context.Background(), 1, true); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if probes.Load() != 1 || len(mailer.sends) != 2 {
		t.Fatalf("duplicate due execution: probes=%d sends=%d", probes.Load(), len(mailer.sends))
	}
}

func TestProxyTaskFinalizesAfterCancellationAndNeverAlertsUnknown(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	ctx, cancel := context.WithCancel(context.Background())
	service := NewProxyTestTaskService(repo, proxyTaskTestFunc(func(context.Context, int64) (*ProxyTestResult, error) {
		cancel()
		return nil, context.Canceled
	}), mailer)
	service.now = func() time.Time { return now }
	_, _ = service.run(ctx, 1, true)
	if repo.task.LastRunStatus != "error" || repo.task.Targets[0].LastNodeStatus != "unknown" || len(mailer.sends) != 0 {
		t.Fatalf("canceled execution must finalize without alerting: %+v", repo.task)
	}
}

func TestProxyTaskPersistenceFailurePreventsUntrackedMail(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	repo.failSaveAt = 2
	service := NewProxyTestTaskService(repo, proxyTaskTestFunc(func(context.Context, int64) (*ProxyTestResult, error) {
		return &ProxyTestResult{NodeStatus: "unhealthy"}, nil
	}), mailer)
	service.now = func() time.Time { return now }
	if _, err := service.run(context.Background(), 1, true); err == nil {
		t.Fatal("failed persistence must be reported")
	}
	if len(mailer.sends) != 0 || repo.task.LastRunStatus != "error" {
		t.Fatal("must finalize and withhold mail when fault persistence fails")
	}
}

func TestProxyTaskRequiresSMTPToEnableButAllowsDisabledDraft(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	mailer.configured = false
	service := NewProxyTestTaskService(repo, nil, mailer)
	service.now = func() time.Time { return now }
	if _, err := service.Update(context.Background(), 1, &ProxyTestTaskInput{}); err == nil {
		t.Fatal("enabling an email task without SMTP must fail")
	}
	disabled := false
	if task, err := service.Update(context.Background(), 1, &ProxyTestTaskInput{Enabled: &disabled}); err != nil || task.Enabled || task.SMTPConfigured {
		t.Fatalf("disabled draft should be allowed without SMTP: task=%+v err=%v", task, err)
	}
}

func TestProxyTaskConfigChangeResetsFaultEvenAfterManualRun(t *testing.T) {
	repo, mailer, now := proxyTaskFixture()
	target := repo.task.Targets[0]
	target.ConsecutiveFailures = 1
	target.NotifiedRecipients = []string{"a@test.example", "b@test.example"}
	target.LastProbeConfigVersion = "00000000000000000001-old-identity"
	version := "00000000000000000002-new-identity"
	service := NewProxyTestTaskService(repo, proxyTaskTestFunc(func(context.Context, int64) (*ProxyTestResult, error) {
		return &ProxyTestResult{NodeStatus: "unhealthy", ProbeConfigVersion: version}, nil
	}), mailer)
	service.now = func() time.Time { return now }
	if _, err := service.Run(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if repo.task.Targets[0].LastProbeConfigVersion != "00000000000000000001-old-identity" {
		t.Fatal("manual run must not obscure scheduled configuration baseline")
	}
	if _, err := service.run(context.Background(), 1, true); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sends) != 2 {
		t.Fatal("new node identity should open a fresh fault notification cycle")
	}
	version = "00000000000000000003-new-identity" // cosmetic update timestamp only
	due := now.Add(-time.Second)
	repo.task.NextRunAt = &due
	if _, err := service.run(context.Background(), 1, true); err != nil {
		t.Fatal(err)
	}
	if len(mailer.sends) != 2 {
		t.Fatal("cosmetic edit must not reopen the notification cycle")
	}
}
