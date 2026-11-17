package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/util/httputil"
)

// Proxy management implementations
func (s *adminServiceImpl) ListProxies(ctx context.Context, page, pageSize int, protocol, status, search string, sortBy, sortOrder string) ([]Proxy, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	proxies, result, err := s.proxyRepo.ListWithFilters(ctx, params, protocol, status, search)
	if err != nil {
		return nil, 0, err
	}
	return proxies, result.Total, nil
}

func (s *adminServiceImpl) ListProxiesWithAccountCount(ctx context.Context, page, pageSize int, protocol, status, search string, sortBy, sortOrder string) ([]ProxyWithAccountCount, int64, error) {
	params := pagination.PaginationParams{Page: page, PageSize: pageSize, SortBy: sortBy, SortOrder: sortOrder}
	proxies, result, err := s.proxyRepo.ListWithFiltersAndAccountCount(ctx, params, protocol, status, search)
	if err != nil {
		return nil, 0, err
	}
	s.attachProxyLatency(ctx, proxies)
	return proxies, result.Total, nil
}

func (s *adminServiceImpl) GetAllProxies(ctx context.Context) ([]Proxy, error) {
	return s.proxyRepo.ListActive(ctx)
}

func (s *adminServiceImpl) GetAllProxiesWithAccountCount(ctx context.Context) ([]ProxyWithAccountCount, error) {
	proxies, err := s.proxyRepo.ListActiveWithAccountCount(ctx)
	if err != nil {
		return nil, err
	}
	s.attachProxyLatency(ctx, proxies)
	return proxies, nil
}

func (s *adminServiceImpl) GetProxy(ctx context.Context, id int64) (*Proxy, error) {
	return s.proxyRepo.GetByID(ctx, id)
}

func (s *adminServiceImpl) GetProxiesByIDs(ctx context.Context, ids []int64) ([]Proxy, error) {
	return s.proxyRepo.ListByIDs(ctx, ids)
}

func (s *adminServiceImpl) CreateProxy(ctx context.Context, input *CreateProxyInput) (*Proxy, error) {
	if !isJSONTimeInRange(input.ExpiresAt) {
		return nil, infraerrors.BadRequest("PROXY_EXPIRY_INVALID", "proxy expiry year must be between 0 and 9999")
	}
	// 规范化 fallback_mode
	mode := input.FallbackMode
	if mode == "" {
		mode = FallbackModeNone
	}
	// 校验：mode=proxy 必须有 backup
	if mode == FallbackModeProxy && input.BackupProxyID == nil {
		return nil, infraerrors.BadRequest("PROXY_BACKUP_REQUIRED", "backup proxy required when fallback_mode=proxy")
	}
	if input.ExpiryWarnDays < 0 {
		return nil, infraerrors.BadRequest("PROXY_WARN_DAYS_INVALID", "expiry_warn_days must be >= 0")
	}

	proxy := &Proxy{
		NodeTestPort:      input.NodeTestPort,
		MihomoPrimaryNode: strings.TrimSpace(input.MihomoPrimaryNode),
		MihomoGroup:       strings.TrimSpace(input.MihomoGroup),
		Name:              input.Name,
		Protocol:          input.Protocol,
		Host:              input.Host,
		Port:              input.Port,
		Username:          input.Username,
		Password:          input.Password,
		Status:            StatusActive,
		ExpiresAt:         input.ExpiresAt,
		FallbackMode:      mode,
		BackupProxyID:     input.BackupProxyID,
		ExpiryWarnDays:    input.ExpiryWarnDays,
	}
	if err := ValidateProxyNodeTestConfig(proxy); err != nil {
		return nil, infraerrors.BadRequest("PROXY_NODE_TEST_CONFIG_INVALID", err.Error())
	}
	if err := s.proxyRepo.Create(ctx, proxy); err != nil {
		return nil, err
	}
	// Probe latency asynchronously so creation isn't blocked by network timeout.
	go s.probeProxyLatency(context.Background(), proxy)
	return proxy, nil
}

func (s *adminServiceImpl) updateProxy(ctx context.Context, id int64, input *UpdateProxyInput) (*Proxy, error) {
	if !isJSONTimeInRange(input.ExpiresAt) {
		return nil, infraerrors.BadRequest("PROXY_EXPIRY_INVALID", "proxy expiry year must be between 0 and 9999")
	}
	// 校验：backup_proxy_id 不能是自身
	if input.BackupProxyID != nil && *input.BackupProxyID == id {
		return nil, infraerrors.BadRequest("PROXY_BACKUP_SELF", "backup proxy cannot be itself")
	}
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	previous := *proxy

	// Merge only supplied fields, then validate the resulting fallback configuration.
	mode := proxy.FallbackMode
	if input.FallbackMode != "" {
		mode = input.FallbackMode
	}
	backupID := proxy.BackupProxyID
	if input.BackupProxyID != nil || input.ClearBackupID {
		backupID = input.BackupProxyID
	}
	if mode == FallbackModeProxy && backupID == nil {
		return nil, infraerrors.BadRequest("PROXY_BACKUP_REQUIRED", "backup proxy required when fallback_mode=proxy")
	}
	if input.ExpiryWarnDays != nil && *input.ExpiryWarnDays < 0 {
		return nil, infraerrors.BadRequest("PROXY_WARN_DAYS_INVALID", "expiry_warn_days must be >= 0")
	}

	if input.Name != "" {
		proxy.Name = input.Name
	}
	if input.Protocol != "" {
		proxy.Protocol = input.Protocol
	}
	if input.Host != "" {
		proxy.Host = input.Host
	}
	if input.Port != 0 {
		proxy.Port = input.Port
	}
	if input.Username != nil {
		proxy.Username = *input.Username
	}
	if input.Password != nil {
		proxy.Password = *input.Password
	}
	if input.Status != "" {
		proxy.Status = input.Status
	}
	if input.ExpiresAt != nil || input.ClearExpiresAt {
		proxy.ExpiresAt = input.ExpiresAt
	}
	proxy.FallbackMode = mode
	proxy.BackupProxyID = backupID
	if input.ExpiryWarnDays != nil {
		proxy.ExpiryWarnDays = *input.ExpiryWarnDays
	}
	if input.ClearNodeTest {
		proxy.NodeTestPort, proxy.MihomoPrimaryNode, proxy.MihomoGroup = nil, "", ""
	} else {
		if input.NodeTestPort != nil {
			proxy.NodeTestPort = input.NodeTestPort
		}
		if input.MihomoPrimaryNode != nil {
			proxy.MihomoPrimaryNode = strings.TrimSpace(*input.MihomoPrimaryNode)
		}
		if input.MihomoGroup != nil {
			proxy.MihomoGroup = strings.TrimSpace(*input.MihomoGroup)
		}
	}
	if err := ValidateProxyNodeTestConfig(proxy); err != nil {
		return nil, infraerrors.BadRequest("PROXY_NODE_TEST_CONFIG_INVALID", err.Error())
	}

	if err := s.proxyRepo.Update(ctx, proxy); err != nil {
		return nil, err
	}
	if err := s.invalidateProxyMonitoring(ctx, &previous, proxy); err != nil {
		return nil, err
	}
	return proxy, nil
}

func (s *adminServiceImpl) deleteProxy(ctx context.Context, id int64) error {
	count, err := s.proxyRepo.CountAccountsByProxyID(ctx, id)
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrProxyInUse
	}
	if err := s.proxyRepo.Delete(ctx, id); err != nil {
		return err
	}
	if s.proxyTaskCoordinator != nil {
		return s.proxyTaskCoordinator.ResetProxyTargets(ctx, id)
	}
	return nil
}

func (s *adminServiceImpl) BatchDeleteProxies(ctx context.Context, ids []int64) (*ProxyBatchDeleteResult, error) {
	result := &ProxyBatchDeleteResult{}
	if len(ids) == 0 {
		return result, nil
	}

	for _, id := range ids {
		count, err := s.proxyRepo.CountAccountsByProxyID(ctx, id)
		if err != nil {
			result.Skipped = append(result.Skipped, ProxyBatchDeleteSkipped{
				ID:     id,
				Reason: err.Error(),
			})
			continue
		}
		if count > 0 {
			result.Skipped = append(result.Skipped, ProxyBatchDeleteSkipped{
				ID:     id,
				Reason: ErrProxyInUse.Error(),
			})
			continue
		}
		if err := s.DeleteProxy(ctx, id); err != nil {
			result.Skipped = append(result.Skipped, ProxyBatchDeleteSkipped{
				ID:     id,
				Reason: err.Error(),
			})
			continue
		}
		result.DeletedIDs = append(result.DeletedIDs, id)
	}

	return result, nil
}

func (s *adminServiceImpl) GetProxyAccounts(ctx context.Context, proxyID int64) ([]ProxyAccountSummary, error) {
	return s.proxyRepo.ListAccountSummariesByProxyID(ctx, proxyID)
}

func (s *adminServiceImpl) CheckProxyExists(ctx context.Context, host string, port int, username, password string) (bool, error) {
	return s.proxyRepo.ExistsByHostPortAuth(ctx, host, port, username, password)
}

func (s *adminServiceImpl) TestProxy(ctx context.Context, id int64) (*ProxyTestResult, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	info := s.refreshProxyHealth(ctx, proxy)
	s.saveProxyLatency(ctx, id, info)
	return proxyTestResultFromSnapshot(info), nil
}

func (s *adminServiceImpl) refreshProxyHealth(ctx context.Context, proxy *Proxy) *ProxyLatencyInfo {
	if proxy.HasNodeTestConfig() {
		opts := proxyHealthOptionsForContext(ctx)
		if s.proxyProber != nil {
			opts.exitInfo = s.proxyProber.ProbeProxy
		}
		return probeMihomoProxy(ctx, proxy, opts)
	}
	info := newProxyProbeSnapshot(proxy)
	info.RouteStatus = ProxyRouteNotApplicable
	if s.proxyProber == nil {
		info.Message = "Proxy probe service is not configured"
		return info
	}
	probeCtx := ctx
	if timeout, ok := ctx.Value(proxyProbeTimeoutKey{}).(time.Duration); ok && timeout > 0 {
		var cancel context.CancelFunc
		probeCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	exitInfo, latency, err := s.proxyProber.ProbeProxy(probeCtx, proxy.NodeTestURL())
	checked := time.Now().UTC()
	info.NodeCheckedAt, info.BusinessCheckedAt = &checked, &checked
	if err != nil {
		info.NodeStatus, info.BusinessStatus, info.Message = ProxyHealthUnhealthy, ProxyHealthUnhealthy, err.Error()
		if ctx.Err() != nil {
			info.NodeStatus, info.BusinessStatus = ProxyHealthUnknown, ProxyHealthUnknown
		}
		return info
	}
	info.Success, info.NodeStatus, info.BusinessStatus = true, ProxyHealthHealthy, ProxyHealthHealthy
	info.LatencyMs, info.BusinessLatencyMs, info.Message = &latency, &latency, "Proxy is accessible"
	if exitInfo != nil {
		info.IPAddress, info.Country, info.CountryCode = exitInfo.IP, exitInfo.Country, exitInfo.CountryCode
		info.Region, info.City = exitInfo.Region, exitInfo.City
	}
	info.UpdatedAt = checked
	return info
}

func (s *adminServiceImpl) CheckProxyQuality(ctx context.Context, id int64) (*ProxyQualityCheckResult, error) {
	proxy, err := s.proxyRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}

	result := &ProxyQualityCheckResult{
		ProxyID:   id,
		Score:     100,
		Grade:     "A",
		CheckedAt: time.Now().Unix(),
		Items:     make([]ProxyQualityCheckItem, 0, len(proxyQualityTargets)+1),
	}

	proxyURL := proxy.NodeTestURL()
	if err := ValidateProxyNodeTestConfig(proxy); err != nil {
		result.Items = append(result.Items, ProxyQualityCheckItem{Target: "configuration", Status: "fail", Message: err.Error()})
		result.FailedCount++
		finalizeProxyQualityResult(result)
		s.saveProxyQualitySnapshot(ctx, proxy, result, nil)
		return result, nil
	}
	if s.proxyProber == nil {
		result.Items = append(result.Items, ProxyQualityCheckItem{
			Target:  "base_connectivity",
			Status:  "fail",
			Message: "代理探测服务未配置",
		})
		result.FailedCount++
		finalizeProxyQualityResult(result)
		s.saveProxyQualitySnapshot(ctx, proxy, result, nil)
		return result, nil
	}

	exitInfo, latencyMs, err := s.proxyProber.ProbeProxy(ctx, proxyURL)
	if err != nil {
		result.Items = append(result.Items, ProxyQualityCheckItem{
			Target:    "base_connectivity",
			Status:    "fail",
			LatencyMs: latencyMs,
			Message:   err.Error(),
		})
		result.FailedCount++
		finalizeProxyQualityResult(result)
		s.saveProxyQualitySnapshot(ctx, proxy, result, nil)
		return result, nil
	}

	result.ExitIP = exitInfo.IP
	result.Country = exitInfo.Country
	result.CountryCode = exitInfo.CountryCode
	result.BaseLatencyMs = latencyMs
	result.Items = append(result.Items, ProxyQualityCheckItem{
		Target:    "base_connectivity",
		Status:    "pass",
		LatencyMs: latencyMs,
		Message:   "代理出口连通正常",
	})
	result.PassedCount++

	client, err := httpclient.GetClient(httpclient.Options{
		ProxyURL:              proxyURL,
		Timeout:               proxyQualityRequestTimeout,
		ResponseHeaderTimeout: proxyQualityResponseHeaderTimeout,
	})
	if err != nil {
		result.Items = append(result.Items, ProxyQualityCheckItem{
			Target:  "http_client",
			Status:  "fail",
			Message: fmt.Sprintf("创建检测客户端失败: %v", err),
		})
		result.FailedCount++
		finalizeProxyQualityResult(result)
		s.saveProxyQualitySnapshot(ctx, proxy, result, exitInfo)
		return result, nil
	}

	for _, target := range proxyQualityTargets {
		item := runProxyQualityTarget(ctx, client, target)
		result.Items = append(result.Items, item)
		switch item.Status {
		case "pass":
			result.PassedCount++
		case "warn":
			result.WarnCount++
		case "challenge":
			result.ChallengeCount++
		default:
			result.FailedCount++
		}
	}

	finalizeProxyQualityResult(result)
	s.saveProxyQualitySnapshot(ctx, proxy, result, exitInfo)
	return result, nil
}

func runProxyQualityTarget(ctx context.Context, client *http.Client, target proxyQualityTarget) ProxyQualityCheckItem {
	item := ProxyQualityCheckItem{
		Target: target.Target,
	}

	req, err := http.NewRequestWithContext(ctx, target.Method, target.URL, nil)
	if err != nil {
		item.Status = "fail"
		item.Message = fmt.Sprintf("构建请求失败: %v", err)
		return item
	}
	req.Header.Set("Accept", "application/json,text/html,*/*")
	req.Header.Set("User-Agent", proxyQualityClientUserAgent)

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		item.Status = "fail"
		item.LatencyMs = time.Since(start).Milliseconds()
		item.Message = fmt.Sprintf("请求失败: %v", err)
		return item
	}
	defer func() { _ = resp.Body.Close() }()
	item.LatencyMs = time.Since(start).Milliseconds()
	item.HTTPStatus = resp.StatusCode

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, proxyQualityMaxBodyBytes+1))
	if readErr != nil {
		item.Status = "fail"
		item.Message = fmt.Sprintf("读取响应失败: %v", readErr)
		return item
	}
	if int64(len(body)) > proxyQualityMaxBodyBytes {
		body = body[:proxyQualityMaxBodyBytes]
	}

	// Cloudflare challenge 检测
	if httputil.IsCloudflareChallengeResponse(resp.StatusCode, resp.Header, body) {
		item.Status = "challenge"
		item.CFRay = httputil.ExtractCloudflareRayID(resp.Header, body)
		item.Message = "命中 Cloudflare challenge"
		return item
	}

	if _, ok := target.AllowedStatuses[resp.StatusCode]; ok {
		// 白名单内的状态码均代表目标可达：2xx 表示接口直接可用，
		// 401/405 等是无鉴权探测的预期结果，同样视为连通正常，不再扣分。
		item.Status = "pass"
		if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
			item.Message = fmt.Sprintf("HTTP %d", resp.StatusCode)
		} else {
			item.Message = fmt.Sprintf("HTTP %d（目标可达）", resp.StatusCode)
		}
		return item
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		item.Status = "warn"
		item.Message = "目标返回 429，可能存在频控"
		return item
	}

	item.Status = "fail"
	item.Message = fmt.Sprintf("非预期状态码: %d", resp.StatusCode)
	return item
}

func finalizeProxyQualityResult(result *ProxyQualityCheckResult) {
	if result == nil {
		return
	}
	score := 100 - result.WarnCount*10 - result.FailedCount*22 - result.ChallengeCount*30
	if score < 0 {
		score = 0
	}
	result.Score = score
	result.Grade = proxyQualityGrade(score)
	result.Summary = fmt.Sprintf(
		"通过 %d 项，告警 %d 项，失败 %d 项，挑战 %d 项",
		result.PassedCount,
		result.WarnCount,
		result.FailedCount,
		result.ChallengeCount,
	)
}

func proxyQualityGrade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}

func proxyQualityOverallStatus(result *ProxyQualityCheckResult) string {
	if result == nil {
		return ""
	}
	if result.ChallengeCount > 0 {
		return "challenge"
	}
	if result.FailedCount > 0 {
		return "failed"
	}
	if result.WarnCount > 0 {
		return "warn"
	}
	if result.PassedCount > 0 {
		return "healthy"
	}
	return "failed"
}

func proxyQualityFirstCFRay(result *ProxyQualityCheckResult) string {
	if result == nil {
		return ""
	}
	for _, item := range result.Items {
		if item.CFRay != "" {
			return item.CFRay
		}
	}
	return ""
}

func proxyQualityBaseConnectivityPass(result *ProxyQualityCheckResult) bool {
	if result == nil {
		return false
	}
	for _, item := range result.Items {
		if item.Target == "base_connectivity" {
			return item.Status == "pass"
		}
	}
	return false
}

func (s *adminServiceImpl) saveProxyQualitySnapshot(ctx context.Context, proxy *Proxy, result *ProxyQualityCheckResult, exitInfo *ProxyExitInfo) {
	if result == nil || proxy == nil {
		return
	}
	info := newProxyProbeSnapshot(proxy)
	info.QualityOnly = true
	if !proxy.HasNodeTestConfig() {
		info.RouteStatus = ProxyRouteNotApplicable
	}
	score, checkedAt := result.Score, result.CheckedAt
	info.QualityStatus, info.QualityScore = proxyQualityOverallStatus(result), &score
	info.QualityGrade, info.QualitySummary = result.Grade, result.Summary
	info.QualityCheckedAt, info.QualityCFRay = &checkedAt, proxyQualityFirstCFRay(result)
	// Quality is a separate diagnostic. Never overwrite node/business/route
	// observations or their sampling identity with a quality request's result.
	if exitInfo != nil {
		info.IPAddress, info.Country, info.CountryCode = exitInfo.IP, exitInfo.Country, exitInfo.CountryCode
		info.Region, info.City = exitInfo.Region, exitInfo.City
	}
	s.saveProxyLatency(ctx, proxy.ID, info)
}

func (s *adminServiceImpl) probeProxyLatency(ctx context.Context, proxy *Proxy) {
	if proxy == nil {
		return
	}
	info := s.refreshProxyHealth(ctx, proxy)
	s.saveProxyLatency(ctx, proxy.ID, info)
}

func (s *adminServiceImpl) attachProxyLatency(ctx context.Context, proxies []ProxyWithAccountCount) {
	ids := make([]int64, 0, len(proxies))
	for i := range proxies {
		proxies[i].NodeStatus, proxies[i].BusinessStatus = ProxyHealthUnknown, ProxyHealthUnknown
		proxies[i].RouteStatus = ProxyHealthUnknown
		if !proxies[i].HasNodeTestConfig() {
			proxies[i].RouteStatus = ProxyRouteNotApplicable
		}
		ids = append(ids, proxies[i].ID)
	}
	if s.proxyLatencyCache == nil || len(ids) == 0 {
		return
	}
	latencies, err := s.proxyLatencyCache.GetProxyLatencies(ctx, ids)
	if err != nil {
		logger.LegacyPrintf("service.admin", "Warning: load proxy latency cache failed: %v", err)
		return
	}
	now := time.Now()
	for i := range proxies {
		info := latencies[proxies[i].ID]
		if info == nil {
			continue
		}
		if info.ProbeConfigVersion != ProxyProbeConfigVersion(&proxies[i].Proxy) && (info.ProbeConfigVersion != "" || proxies[i].HasNodeTestConfig()) {
			continue
		}
		// Work on a copy: expiry affects presentation, never a task's fault cycle.
		value := *info
		if value.NodeStatus == "" && !proxies[i].HasNodeTestConfig() {
			if value.Success {
				value.NodeStatus = ProxyHealthHealthy
			} else {
				value.NodeStatus = ProxyHealthUnhealthy
			}
			if !value.UpdatedAt.IsZero() {
				value.NodeCheckedAt = &value.UpdatedAt
			}
			value.BusinessStatus, value.BusinessCheckedAt = value.NodeStatus, value.NodeCheckedAt
			value.BusinessLatencyMs, value.RouteStatus = value.LatencyMs, ProxyRouteNotApplicable
		}
		if value.NodeCheckedAt == nil || now.Sub(*value.NodeCheckedAt) > ProxyHealthMaxAge {
			value.NodeStatus = ProxyHealthUnknown
		}
		if value.BusinessCheckedAt == nil || now.Sub(*value.BusinessCheckedAt) > ProxyHealthMaxAge {
			value.BusinessStatus = ProxyHealthUnknown
		}
		if value.RouteStatus != ProxyRouteNotApplicable && (value.RouteCheckedAt == nil || now.Sub(*value.RouteCheckedAt) > ProxyHealthMaxAge) {
			value.RouteStatus, value.PreferredNode, value.IsBackup, value.RouteFixed = ProxyHealthUnknown, "", false, false
		}
		proxies[i].LatencyStatus = "unknown"
		if value.NodeStatus == ProxyHealthHealthy {
			proxies[i].LatencyStatus, proxies[i].LatencyMs = "success", value.LatencyMs
		}
		if value.NodeStatus == ProxyHealthUnhealthy {
			proxies[i].LatencyStatus = "failed"
		}
		proxies[i].LatencyMessage = value.Message
		proxies[i].IPAddress, proxies[i].Country, proxies[i].CountryCode = value.IPAddress, value.Country, value.CountryCode
		proxies[i].Region, proxies[i].City = value.Region, value.City
		proxies[i].QualityStatus, proxies[i].QualityScore, proxies[i].QualityGrade = value.QualityStatus, value.QualityScore, value.QualityGrade
		proxies[i].QualitySummary, proxies[i].QualityChecked = value.QualitySummary, value.QualityCheckedAt
		proxies[i].NodeStatus, proxies[i].NodeCheckedAt = value.NodeStatus, value.NodeCheckedAt
		proxies[i].BusinessStatus, proxies[i].BusinessLatencyMs, proxies[i].BusinessCheckedAt = value.BusinessStatus, value.BusinessLatencyMs, value.BusinessCheckedAt
		proxies[i].PreferredNode, proxies[i].IsBackup, proxies[i].RouteStatus = value.PreferredNode, value.IsBackup, value.RouteStatus
		proxies[i].RouteCheckedAt, proxies[i].RouteError, proxies[i].RouteFixed = value.RouteCheckedAt, value.RouteError, value.RouteFixed
		proxies[i].SampleID, proxies[i].SampledPod, proxies[i].ProbeConfigVersion = value.SampleID, value.SampledPod, value.ProbeConfigVersion
	}
}

func (s *adminServiceImpl) saveProxyLatency(ctx context.Context, proxyID int64, info *ProxyLatencyInfo) {
	if s.proxyLatencyCache == nil || info == nil {
		return
	}
	if s.proxyRepo != nil && info.ProbeConfigVersion != "" {
		current, err := s.proxyRepo.GetByID(ctx, proxyID)
		if err != nil || current == nil || ProxyProbeConfigVersion(current) != info.ProbeConfigVersion {
			return
		}
	}
	merged := *info
	if latencies, err := s.proxyLatencyCache.GetProxyLatencies(ctx, []int64{proxyID}); err == nil {
		if existing := latencies[proxyID]; existing != nil && existing.ProbeConfigVersion == merged.ProbeConfigVersion {
			if merged.QualityOnly {
				// Keep the complete latest health round even if a quality request
				// originally read an older sample. Redis repeats this atomically.
				quality := merged
				merged = *existing
				merged.QualityOnly = true
				merged.QualityStatus, merged.QualityScore, merged.QualityGrade = quality.QualityStatus, quality.QualityScore, quality.QualityGrade
				merged.QualitySummary, merged.QualityCheckedAt, merged.QualityCFRay = quality.QualitySummary, quality.QualityCheckedAt, quality.QualityCFRay
				merged.IPAddress, merged.Country, merged.CountryCode, merged.Region, merged.City = quality.IPAddress, quality.Country, quality.CountryCode, quality.Region, quality.City
			} else if existing.SampleID > merged.SampleID {
				return
			}
			if merged.QualityCheckedAt == nil && merged.QualityScore == nil && merged.QualityGrade == "" && merged.QualityStatus == "" && merged.QualitySummary == "" && merged.QualityCFRay == "" {
				merged.QualityStatus, merged.QualityScore, merged.QualityGrade = existing.QualityStatus, existing.QualityScore, existing.QualityGrade
				merged.QualitySummary, merged.QualityCheckedAt, merged.QualityCFRay = existing.QualitySummary, existing.QualityCheckedAt, existing.QualityCFRay
			}
		}
	}
	if err := s.proxyLatencyCache.SetProxyLatency(ctx, proxyID, &merged); err != nil {
		logger.LegacyPrintf("service.admin", "Warning: store proxy latency cache failed: %v", err)
	}
}
