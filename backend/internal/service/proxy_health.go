package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/proxyutil"
	"github.com/google/uuid"
)

const (
	ProxyHealthHealthy      = "healthy"
	ProxyHealthUnhealthy    = "unhealthy"
	ProxyHealthUnknown      = "unknown"
	ProxyRouteNotApplicable = "not_applicable"
	ProxyRouteSwitching     = "switching"
	ProxyHealthMaxAge       = 15 * time.Minute
	proxyHealthTargetURL    = "https://cp.cloudflare.com/generate_204"
	proxyControllerURL      = "http://127.0.0.1:9090/proxies"
)

// NodeTestURL deliberately bypasses a fallback listener while preserving the
// original leaf's dialer-proxy chain inside Mihomo.
func (p *Proxy) NodeTestURL() string {
	if p.NodeTestPort != nil {
		return "socks5h://127.0.0.1:" + strconv.Itoa(*p.NodeTestPort)
	}
	return p.URL()
}

func (p *Proxy) HasNodeTestConfig() bool {
	return p.NodeTestPort != nil || p.MihomoPrimaryNode != "" || p.MihomoGroup != ""
}

// ValidateProxyNodeTestConfig only accepts the five fixed loopback bindings in
// the production configuration. Ordinary proxies retain their existing path.
func ValidateProxyNodeTestConfig(p *Proxy) error {
	if p == nil {
		return fmt.Errorf("proxy is required")
	}
	if !p.HasNodeTestConfig() {
		return nil
	}
	if p.NodeTestPort == nil || p.MihomoPrimaryNode == "" || p.MihomoGroup == "" {
		return fmt.Errorf("node_test_port, mihomo_primary_node and mihomo_group must be configured together")
	}
	host := strings.Trim(p.Host, "[]")
	ip := net.ParseIP(host)
	if !strings.EqualFold(host, "localhost") && (ip == nil || !ip.IsLoopback()) {
		return fmt.Errorf("Mihomo node testing requires a loopback business proxy")
	}
	bindings := map[string][3]int{
		"us1": {17903, 7893, 7903}, "us2": {17905, 7895, 7905},
		"us3": {17906, 7896, 7906}, "us4": {17907, 7897, 7907}, "us5": {17908, 7898, 7908},
	}
	binding, ok := bindings[p.MihomoPrimaryNode]
	if !ok || *p.NodeTestPort != binding[0] || p.MihomoGroup != p.MihomoPrimaryNode+"-fallback" {
		return fmt.Errorf("node test port, original node and fallback group do not match a supported US binding")
	}
	if p.Port != binding[1] && p.Port != binding[2] {
		return fmt.Errorf("business port does not match the original US node")
	}
	return nil
}

// ProxyProbeConfigVersion includes an ordered update timestamp and a digest,
// never plaintext credentials. Cache writes can reject a superseded version.
func ProxyProbeConfigVersion(p *Proxy) string {
	if p == nil {
		return ""
	}
	port := 0
	if p.NodeTestPort != nil {
		port = *p.NodeTestPort
	}
	b, _ := json.Marshal([]any{p.Protocol, p.Host, p.Port, p.Username, p.Password, p.Status, port, p.MihomoPrimaryNode, p.MihomoGroup})
	digest := sha256.Sum256(b)
	var stamp int64
	if !p.UpdatedAt.IsZero() {
		// lib/pq sends nanosecond text, but PostgreSQL timestamptz stores
		// microseconds. Its ParseFractionalSecond uses rint(frac * 1e6),
		// including ties-to-even and a possible carry into the next second.
		// Normalize the builder value exactly as the subsequent DB read does;
		// otherwise a post-update fence can permanently reject fresh DB probes.
		fraction := float64(p.UpdatedAt.Nanosecond()) / 1e9
		stamp = p.UpdatedAt.Unix()*1e6 + int64(math.RoundToEven(fraction*1e6))
	}
	return fmt.Sprintf("%020d-%x", stamp, digest)
}

type proxyHealthProbeOptions struct {
	targetURL     string
	controllerURL string
	timeout       time.Duration
	head          func(context.Context, string, string, time.Duration) (int64, error)
}

func defaultProxyHealthProbeOptions() proxyHealthProbeOptions {
	return proxyHealthProbeOptions{targetURL: proxyHealthTargetURL, controllerURL: proxyControllerURL, timeout: 10 * time.Second, head: probeFreshProxyHEAD}
}

type proxyProbeTimeoutKey struct{}

// WithProxyProbeTimeout sets the budget of each independent node/business probe.
// The caller should also apply an overall deadline to the complete sampling round.
func WithProxyProbeTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, proxyProbeTimeoutKey{}, timeout)
}

func proxyHealthOptionsForContext(ctx context.Context) proxyHealthProbeOptions {
	opts := defaultProxyHealthProbeOptions()
	if timeout, ok := ctx.Value(proxyProbeTimeoutKey{}).(time.Duration); ok && timeout > 0 && timeout <= 30*time.Second {
		opts.timeout = timeout
	}
	return opts
}

func newProxyProbeSnapshot(p *Proxy) *ProxyLatencyInfo {
	now := time.Now().UTC()
	pod, _ := os.Hostname()
	return &ProxyLatencyInfo{
		NodeStatus: ProxyHealthUnknown, BusinessStatus: ProxyHealthUnknown, RouteStatus: ProxyHealthUnknown,
		SampleID: fmt.Sprintf("%020d-%s", now.UnixNano(), uuid.NewString()), SampledPod: pod,
		ProbeConfigVersion: ProxyProbeConfigVersion(p), UpdatedAt: now,
	}
}

func probeMihomoProxy(ctx context.Context, p *Proxy, opts proxyHealthProbeOptions) *ProxyLatencyInfo {
	info := newProxyProbeSnapshot(p)
	if err := ValidateProxyNodeTestConfig(p); err != nil {
		info.Message = err.Error()
		return info
	}
	before, beforeErr := readMihomoRoute(ctx, opts.controllerURL, p)
	latency, nodeErr := opts.head(ctx, p.NodeTestURL(), opts.targetURL, opts.timeout)
	checked := time.Now().UTC()
	info.NodeCheckedAt = &checked
	info.NodeStatus = ProxyHealthHealthy
	info.Message = "Original node is accessible"
	if nodeErr != nil {
		info.NodeStatus = ProxyHealthUnhealthy
		info.Message = nodeErr.Error()
	} else {
		info.Success, info.LatencyMs = true, &latency
	}
	if ctx.Err() != nil {
		info.NodeStatus, info.Success = ProxyHealthUnknown, false
	}
	businessLatency, businessErr := opts.head(ctx, p.URL(), opts.targetURL, opts.timeout)
	businessChecked := time.Now().UTC()
	info.BusinessCheckedAt = &businessChecked
	info.BusinessStatus = ProxyHealthHealthy
	if businessErr != nil {
		info.BusinessStatus = ProxyHealthUnhealthy
	} else {
		info.BusinessLatencyMs = &businessLatency
	}
	if ctx.Err() != nil {
		info.BusinessStatus = ProxyHealthUnknown
	}
	after, afterErr := readMihomoRoute(ctx, opts.controllerURL, p)
	routeChecked := time.Now().UTC()
	info.RouteCheckedAt = &routeChecked
	switch {
	case beforeErr != nil:
		info.RouteError = beforeErr.Error()
	case afterErr != nil:
		info.RouteError = afterErr.Error()
	case before.now != after.now || before.fixed != after.fixed:
		info.RouteStatus, info.RouteError = ProxyRouteSwitching, "Group preference changed during the probe"
	default:
		info.RouteStatus, info.PreferredNode, info.RouteFixed = ProxyHealthHealthy, after.now, after.fixed
		info.IsBackup = after.now != p.MihomoPrimaryNode
	}
	info.UpdatedAt = time.Now().UTC()
	return info
}

// Each call owns its Transport: it cannot reuse a business HTTP/2 or proxy
// connection, and it never falls back to a direct connection after proxy error.
func probeFreshProxyHEAD(ctx context.Context, proxyURL, targetURL string, timeout time.Duration) (int64, error) {
	transport := &http.Transport{
		DialContext:       (&net.Dialer{Timeout: timeout}).DialContext,
		DisableKeepAlives: true, TLSHandshakeTimeout: timeout,
		ResponseHeaderTimeout: timeout, ForceAttemptHTTP2: false,
	}
	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err != nil {
			return 0, fmt.Errorf("invalid probe proxy URL")
		}
		if err := proxyutil.ConfigureTransportProxy(transport, parsed); err != nil {
			return 0, fmt.Errorf("invalid probe proxy configuration: %w", err)
		}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, targetURL, nil)
	if err != nil {
		return 0, fmt.Errorf("invalid health target: %w", err)
	}
	start := time.Now()
	resp, err := client.Do(req)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		// Do not return a url.Error that can expose proxy URL userinfo.
		if ue, ok := err.(*url.Error); ok {
			err = ue.Err
		}
		return latency, fmt.Errorf("original-node health target request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent {
		return latency, fmt.Errorf("health target returned HTTP %d; expected 204", resp.StatusCode)
	}
	return latency, nil
}

type mihomoRouteSample struct {
	now   string
	fixed bool
}

func readMihomoRoute(ctx context.Context, endpoint string, p *Proxy) (mihomoRouteSample, error) {
	var sample mihomoRouteSample
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, DisableKeepAlives: true}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return sample, fmt.Errorf("invalid controller endpoint")
	}
	resp, err := client.Do(req)
	if err != nil {
		return sample, fmt.Errorf("Mihomo controller unavailable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return sample, fmt.Errorf("Mihomo controller HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Proxies map[string]struct {
			Type  string   `json:"type"`
			Now   string   `json:"now"`
			All   []string `json:"all"`
			Fixed string   `json:"fixed"`
		} `json:"proxies"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&payload); err != nil {
		return sample, fmt.Errorf("invalid Mihomo controller response")
	}
	group, ok := payload.Proxies[p.MihomoGroup]
	if !ok || !strings.EqualFold(group.Type, "fallback") || len(group.All) == 0 || group.All[0] != p.MihomoPrimaryNode {
		return sample, fmt.Errorf("Mihomo fallback group does not match the original node")
	}
	valid := false
	for _, node := range group.All {
		if node == group.Now {
			valid = true
			break
		}
	}
	if !valid {
		return sample, fmt.Errorf("Mihomo selected node is absent from group")
	}
	return mihomoRouteSample{now: group.Now, fixed: group.Fixed != ""}, nil
}

func proxyTestResultFromSnapshot(info *ProxyLatencyInfo) *ProxyTestResult {
	result := &ProxyTestResult{Success: info.Success, Message: info.Message, IPAddress: info.IPAddress, Country: info.Country, CountryCode: info.CountryCode, Region: info.Region, City: info.City,
		NodeStatus: info.NodeStatus, NodeCheckedAt: info.NodeCheckedAt, BusinessStatus: info.BusinessStatus, BusinessLatencyMs: info.BusinessLatencyMs, BusinessCheckedAt: info.BusinessCheckedAt,
		PreferredNode: info.PreferredNode, IsBackup: info.IsBackup, RouteStatus: info.RouteStatus, RouteCheckedAt: info.RouteCheckedAt, RouteError: info.RouteError, RouteFixed: info.RouteFixed,
		SampleID: info.SampleID, SampledPod: info.SampledPod, ProbeConfigVersion: info.ProbeConfigVersion}
	if info.LatencyMs != nil {
		result.LatencyMs = *info.LatencyMs
	}
	return result
}
