package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func healthTestProxy() *Proxy {
	port := 17903
	return &Proxy{ID: 1, Protocol: "socks5", Host: "127.0.0.1", Port: 7903, NodeTestPort: &port, MihomoPrimaryNode: "us1", MihomoGroup: "us1-fallback", UpdatedAt: time.Now().UTC()}
}

func healthTestController(t *testing.T, now, fixed string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		_, _ = fmt.Fprintf(w, `{"proxies":{"us1-fallback":{"type":"Fallback","now":%q,"fixed":%q,"all":["us1","us2","us3","us4","us5"]}}}`, now, fixed)
	}))
	t.Cleanup(server.Close)
	return server
}

func TestMihomoProxyOriginalFailureDoesNotHideBehindBackup(t *testing.T) {
	p := healthTestProxy()
	controller := healthTestController(t, "us2", "")
	var paths []string
	var geoCalls atomic.Int32
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL = controller.URL
	opts.exitInfo = func(context.Context, string) (*ProxyExitInfo, int64, error) {
		geoCalls.Add(1)
		return &ProxyExitInfo{IP: "192.0.2.2", CountryCode: "US"}, 0, nil
	}
	opts.head = func(_ context.Context, proxyURL, target string, _ time.Duration) (int64, error) {
		paths = append(paths, proxyURL)
		require.Equal(t, proxyHealthTargetURL, target)
		if proxyURL == p.NodeTestURL() {
			return 0, errors.New("original node unavailable")
		}
		return 12, nil
	}
	info := probeMihomoProxy(context.Background(), p, opts)
	require.Equal(t, []string{"socks5h://127.0.0.1:17903", "socks5://127.0.0.1:7903"}, paths)
	require.False(t, info.Success)
	require.Equal(t, ProxyHealthUnhealthy, info.NodeStatus)
	require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
	require.Equal(t, "us2", info.PreferredNode)
	require.True(t, info.IsBackup)
	require.NotNil(t, info.NodeCheckedAt)
	require.NotNil(t, info.BusinessCheckedAt)
	require.NotNil(t, info.RouteCheckedAt)
	require.NotEmpty(t, info.SampleID)
	require.Zero(t, geoCalls.Load(), "a failed original node must not borrow geolocation from the fallback entry")
	require.Empty(t, info.IPAddress)
	require.Empty(t, info.CountryCode)
}

func TestMihomoControllerFailureOnlyMakesRouteUnknown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
	defer server.Close()
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL = server.URL
	opts.head = func(context.Context, string, string, time.Duration) (int64, error) { return 1, nil }
	info := probeMihomoProxy(context.Background(), healthTestProxy(), opts)
	require.True(t, info.Success)
	require.Equal(t, ProxyHealthHealthy, info.NodeStatus)
	require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
	require.Equal(t, ProxyHealthUnknown, info.RouteStatus)
	require.Empty(t, info.PreferredNode)
	require.Contains(t, info.RouteError, "401")
}

func TestMihomoOriginalNodeGeolocationRunsAlongsideBusinessProbe(t *testing.T) {
	p := healthTestProxy()
	controller := healthTestController(t, "us1", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	geoStarted := make(chan string, 1)
	businessStarted := make(chan struct{})
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL = controller.URL
	opts.exitInfo = func(ctx context.Context, proxyURL string) (*ProxyExitInfo, int64, error) {
		geoStarted <- proxyURL
		select {
		case <-businessStarted:
			return &ProxyExitInfo{IP: "192.0.2.7", Country: "United States", CountryCode: "US", Region: "California", City: "Los Angeles"}, 9999, nil
		case <-ctx.Done():
			return nil, 0, ctx.Err()
		}
	}
	opts.head = func(ctx context.Context, proxyURL, _ string, _ time.Duration) (int64, error) {
		if proxyURL == p.URL() {
			select {
			case geoURL := <-geoStarted:
				require.Equal(t, p.NodeTestURL(), geoURL, "geolocation must use the raw listener, never the fallback entry")
				close(businessStarted)
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return 12, nil
	}
	info := probeMihomoProxy(ctx, p, opts)
	require.NoError(t, ctx.Err(), "geolocation and the business check must not wait for each other sequentially")
	require.True(t, info.Success)
	require.EqualValues(t, 12, *info.LatencyMs, "IP lookup latency must not replace the original-node HEAD latency")
	require.Equal(t, ProxyHealthHealthy, info.NodeStatus)
	require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
	require.Equal(t, "192.0.2.7", info.IPAddress)
	require.Equal(t, "United States", info.Country)
	require.Equal(t, "US", info.CountryCode)
	require.Equal(t, "California", info.Region)
	require.Equal(t, "Los Angeles", info.City)
	result := proxyTestResultFromSnapshot(info)
	require.Equal(t, info.IPAddress, result.IPAddress)
	require.Equal(t, info.Country, result.Country)
	require.Equal(t, info.City, result.City)
}

func TestMihomoGeolocationFailureDoesNotChangeHealthyConnectivity(t *testing.T) {
	controller := healthTestController(t, "us1", "")
	for _, outcome := range []string{"failure", "timeout", "empty"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			opts := defaultProxyHealthProbeOptions()
			opts.controllerURL, opts.timeout = controller.URL, 20*time.Millisecond
			opts.head = func(context.Context, string, string, time.Duration) (int64, error) { return 12, nil }
			opts.exitInfo = func(ctx context.Context, _ string) (*ProxyExitInfo, int64, error) {
				switch outcome {
				case "failure":
					return &ProxyExitInfo{IP: "192.0.2.99"}, 0, errors.New("geolocation provider unavailable")
				case "timeout":
					<-ctx.Done()
					return nil, 0, ctx.Err()
				default:
					return nil, 0, nil
				}
			}
			info := probeMihomoProxy(ctx, healthTestProxy(), opts)
			require.NoError(t, ctx.Err(), "optional lookup must have its own bounded deadline")
			require.True(t, info.Success)
			require.Equal(t, ProxyHealthHealthy, info.NodeStatus)
			require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
			require.Equal(t, ProxyHealthHealthy, info.RouteStatus)
			require.EqualValues(t, 12, *info.LatencyMs)
			require.Empty(t, info.IPAddress)
			require.Empty(t, info.Country)
		})
	}
}

func TestMihomoFailedOriginalDoesNotUseBackupGeolocation(t *testing.T) {
	p := healthTestProxy()
	controller := healthTestController(t, "us2", "")
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL = controller.URL
	opts.head = func(_ context.Context, proxyURL, _ string, _ time.Duration) (int64, error) {
		if proxyURL == p.NodeTestURL() {
			return 0, errors.New("original node unavailable")
		}
		return 12, nil
	}
	var geoCalls atomic.Int32
	opts.exitInfo = func(context.Context, string) (*ProxyExitInfo, int64, error) {
		geoCalls.Add(1)
		return &ProxyExitInfo{IP: "192.0.2.8", CountryCode: "US"}, 0, nil
	}
	info := probeMihomoProxy(context.Background(), p, opts)
	require.Zero(t, geoCalls.Load())
	require.False(t, info.Success)
	require.Equal(t, ProxyHealthUnhealthy, info.NodeStatus)
	require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
	require.Equal(t, "us2", info.PreferredNode)
	require.True(t, info.IsBackup)
	require.Empty(t, info.IPAddress)
	require.Empty(t, info.CountryCode)
}

func TestMihomoGeolocationCompletedBeforeDeadlineSurvivesSlowerBusinessProbe(t *testing.T) {
	p := healthTestProxy()
	controller := healthTestController(t, "us1", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	geoCtxReady := make(chan context.Context, 1)
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL, opts.timeout = controller.URL, 50*time.Millisecond
	opts.exitInfo = func(ctx context.Context, _ string) (*ProxyExitInfo, int64, error) {
		geoCtxReady <- ctx
		return &ProxyExitInfo{IP: "192.0.2.7", CountryCode: "US"}, 0, nil
	}
	opts.head = func(ctx context.Context, proxyURL, _ string, _ time.Duration) (int64, error) {
		if proxyURL == p.URL() {
			select {
			case geoCtx := <-geoCtxReady:
				<-geoCtx.Done()
			case <-ctx.Done():
				return 0, ctx.Err()
			}
		}
		return 12, nil
	}
	info := probeMihomoProxy(ctx, p, opts)
	require.NoError(t, ctx.Err())
	require.True(t, info.Success)
	require.Equal(t, "192.0.2.7", info.IPAddress)
	require.Equal(t, "US", info.CountryCode)
}

func TestMihomoControllerSwitchingAndManualSelection(t *testing.T) {
	t.Run("changing preference is not attributed to one exit", func(t *testing.T) {
		var count atomic.Int32
		controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			node := "us1"
			if count.Add(1) > 1 {
				node = "us2"
			}
			_, _ = fmt.Fprintf(w, `{"proxies":{"us1-fallback":{"type":"Fallback","now":%q,"all":["us1","us2"]}}}`, node)
		}))
		defer controller.Close()
		opts := defaultProxyHealthProbeOptions()
		opts.controllerURL = controller.URL
		opts.head = func(context.Context, string, string, time.Duration) (int64, error) { return 1, nil }
		info := probeMihomoProxy(context.Background(), healthTestProxy(), opts)
		require.Equal(t, ProxyRouteSwitching, info.RouteStatus)
		require.Empty(t, info.PreferredNode)
	})
	t.Run("manual selection remains explicit", func(t *testing.T) {
		controller := healthTestController(t, "us2", "us2")
		opts := defaultProxyHealthProbeOptions()
		opts.controllerURL = controller.URL
		opts.head = func(context.Context, string, string, time.Duration) (int64, error) { return 1, nil }
		info := probeMihomoProxy(context.Background(), healthTestProxy(), opts)
		require.True(t, info.RouteFixed)
		require.True(t, info.IsBackup)
	})
}

func TestFreshProxyHEADRequires204AndDoesNotFollowRedirects(t *testing.T) {
	var redirected atomic.Int32
	var connections []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodHead, r.Method)
		connections = append(connections, r.RemoteAddr)
		switch r.URL.Path {
		case "/redirect":
			w.Header().Set("Location", "/ok")
			w.WriteHeader(http.StatusFound)
		case "/ok":
			redirected.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	_, err := probeFreshProxyHEAD(context.Background(), "", server.URL+"/redirect", time.Second)
	require.ErrorContains(t, err, "HTTP 302")
	require.EqualValues(t, 0, redirected.Load())
	_, err = probeFreshProxyHEAD(context.Background(), "", server.URL+"/forbidden", time.Second)
	require.ErrorContains(t, err, "HTTP 403")
	for i := 0; i < 2; i++ {
		_, err = probeFreshProxyHEAD(context.Background(), "", server.URL+"/ok", time.Second)
		require.NoError(t, err)
	}
	require.NotEqual(t, connections[2], connections[3], "independent probes must use new TCP connections")
}

func TestFreshProxyHEADKeepsTLSVerification(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	_, err := probeFreshProxyHEAD(context.Background(), "", server.URL, time.Second)
	require.Error(t, err, "untrusted TLS certificates must fail the health check")
}

func TestMihomoProbeTimeoutIsFailureButParentCancellationIsUnknown(t *testing.T) {
	controller := healthTestController(t, "us2", "")
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL = controller.URL
	opts.timeout = 10 * time.Millisecond
	opts.head = func(ctx context.Context, _ string, _ string, timeout time.Duration) (int64, error) {
		probeCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		<-probeCtx.Done()
		return 0, probeCtx.Err()
	}
	info := probeMihomoProxy(context.Background(), healthTestProxy(), opts)
	require.Equal(t, ProxyHealthUnhealthy, info.NodeStatus, "a real per-probe timeout must participate in failure alerts")
	require.Equal(t, ProxyHealthUnhealthy, info.BusinessStatus)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	info = probeMihomoProxy(ctx, healthTestProxy(), opts)
	require.Equal(t, ProxyHealthUnknown, info.NodeStatus, "cancelled application work is not evidence of a failed original node")
	require.Equal(t, ProxyHealthUnknown, info.BusinessStatus)
}

func TestNodeTestConfigValidationAndRawURL(t *testing.T) {
	p := healthTestProxy()
	p.Username, p.Password = "business-user", "secret"
	require.NoError(t, ValidateProxyNodeTestConfig(p))
	require.Equal(t, "socks5h://127.0.0.1:17903", p.NodeTestURL())
	// Names, groups and ports are independent operator-supplied values.
	port := 28007
	p = &Proxy{Protocol: "http", Host: "localhost", Port: 8123, NodeTestPort: &port,
		MihomoPrimaryNode: "US 7 西部", MihomoGroup: "GPT exits"}
	require.NoError(t, ValidateProxyNodeTestConfig(p))
	require.Equal(t, "socks5h://127.0.0.1:28007", p.NodeTestURL())
	p = &Proxy{Protocol: "https", Host: "example.com", Port: 443}
	require.NoError(t, ValidateProxyNodeTestConfig(p))
	require.Equal(t, p.URL(), p.NodeTestURL())
}

func TestNodeTestConfigRejectsInvalidCustomBindings(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Proxy)
	}{
		{"missing port", func(p *Proxy) { p.NodeTestPort = nil }},
		{"missing original node", func(p *Proxy) { p.MihomoPrimaryNode = "" }},
		{"blank original node", func(p *Proxy) { p.MihomoPrimaryNode = " \t " }},
		{"missing group", func(p *Proxy) { p.MihomoGroup = "" }},
		{"blank group", func(p *Proxy) { p.MihomoGroup = " \n " }},
		{"zero port", func(p *Proxy) { v := 0; p.NodeTestPort = &v }},
		{"negative port", func(p *Proxy) { v := -1; p.NodeTestPort = &v }},
		{"port too high", func(p *Proxy) { v := 65536; p.NodeTestPort = &v }},
		{"business listener as node test", func(p *Proxy) { v := p.Port; p.NodeTestPort = &v }},
		{"remote business proxy", func(p *Proxy) { p.Host = "remote.example" }},
		{"long original node", func(p *Proxy) { p.MihomoPrimaryNode = strings.Repeat("a", 101) }},
		{"long group in UTF-8 bytes", func(p *Proxy) { p.MihomoGroup = strings.Repeat("美", 34) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := healthTestProxy()
			tc.mutate(p)
			require.Error(t, ValidateProxyNodeTestConfig(p))
		})
	}
	require.Error(t, ValidateProxyNodeTestConfig(nil))
}

func TestCustomNodeProbeKeepsOriginalFailureSeparateFromFallback(t *testing.T) {
	port := 28007
	p := &Proxy{Protocol: "socks5", Host: "127.0.0.1", Port: 8123, NodeTestPort: &port,
		MihomoPrimaryNode: "US 7 西部", MihomoGroup: "GPT exits"}
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{
			p.MihomoGroup: map[string]any{"type": "Fallback", "all": []string{p.MihomoPrimaryNode, "US 8 备用"}, "now": "US 8 备用"},
		}}))
	}))
	defer controller.Close()
	var requested []string
	info := probeMihomoProxy(context.Background(), p, proxyHealthProbeOptions{
		controllerURL: controller.URL, timeout: time.Second,
		head: func(_ context.Context, proxyURL, _ string, _ time.Duration) (int64, error) {
			requested = append(requested, proxyURL)
			if proxyURL == p.NodeTestURL() {
				return 0, errors.New("original node unavailable")
			}
			return 17, nil
		},
	})
	require.Equal(t, []string{"socks5h://127.0.0.1:28007", "socks5://127.0.0.1:8123"}, requested)
	require.Equal(t, ProxyHealthUnhealthy, info.NodeStatus)
	require.False(t, info.Success)
	require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
	require.Equal(t, ProxyHealthHealthy, info.RouteStatus)
	require.Equal(t, "US 8 备用", info.PreferredNode)
	require.True(t, info.IsBackup)
}

func TestCustomNodeProbeWithMismatchedGroupOnlyMakesRouteUnknown(t *testing.T) {
	port := 28008
	p := &Proxy{Protocol: "socks5", Host: "127.0.0.1", Port: 8124, NodeTestPort: &port,
		MihomoPrimaryNode: "us8", MihomoGroup: "GPT custom pool"}
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"proxies": map[string]any{
			p.MihomoGroup: map[string]any{"type": "Fallback", "all": []string{"another original", "us8"}, "now": "us8"},
		}})
	}))
	defer controller.Close()
	var requested []string
	info := probeMihomoProxy(context.Background(), p, proxyHealthProbeOptions{
		controllerURL: controller.URL, timeout: time.Second,
		head: func(_ context.Context, proxyURL, _ string, _ time.Duration) (int64, error) {
			requested = append(requested, proxyURL)
			return 12, nil
		},
	})
	require.Equal(t, []string{"socks5h://127.0.0.1:28008", "socks5://127.0.0.1:8124"}, requested)
	require.Equal(t, ProxyHealthHealthy, info.NodeStatus)
	require.Equal(t, ProxyHealthHealthy, info.BusinessStatus)
	require.Equal(t, ProxyHealthUnknown, info.RouteStatus)
	require.Empty(t, info.PreferredNode)
	require.Contains(t, info.RouteError, "does not match the original node")
}

type healthMemoryCache struct {
	value  *ProxyLatencyInfo
	writes int
}

func (c *healthMemoryCache) GetProxyLatencies(context.Context, []int64) (map[int64]*ProxyLatencyInfo, error) {
	return map[int64]*ProxyLatencyInfo{1: c.value}, nil
}
func (c *healthMemoryCache) SetProxyLatency(_ context.Context, _ int64, value *ProxyLatencyInfo) error {
	copy := *value
	c.value = &copy
	c.writes++
	return nil
}

type healthCurrentProxyRepo struct {
	ProxyRepository
	proxy *Proxy
}

func (r *healthCurrentProxyRepo) GetByID(context.Context, int64) (*Proxy, error) { return r.proxy, nil }

func TestProxyHealthCacheRejectsOldConfigurationAndSamplingRound(t *testing.T) {
	p := healthTestProxy()
	newInfo := newProxyProbeSnapshot(p)
	cache := &healthMemoryCache{value: newInfo}
	s := &adminServiceImpl{proxyRepo: &healthCurrentProxyRepo{proxy: p}, proxyLatencyCache: cache}
	oldRound := *newInfo
	oldRound.SampleID = "00000000000000000000-old"
	s.saveProxyLatency(context.Background(), p.ID, &oldRound)
	require.Zero(t, cache.writes)
	oldConfig := *p
	oldConfig.Password = "old-secret"
	s.saveProxyLatency(context.Background(), p.ID, newProxyProbeSnapshot(&oldConfig))
	require.Zero(t, cache.writes)
	require.NotContains(t, ProxyProbeConfigVersion(&oldConfig), "old-secret")
}

func TestProxyProbeConfigVersionMatchesPostgresTimestampRoundtrip(t *testing.T) {
	// Expected values follow PostgreSQL ParseFractionalSecond's
	// rint(frac * 1000000), after lib/pq sends its nanosecond text.
	tests := []struct {
		name               string
		nanoseconds        int
		storedMicroseconds int
	}{
		{"round down", 123456123, 123456},
		{"round up", 123456789, 123457},
		{"tie to even below", 123456500, 123456},
		{"tie to even above", 123457500, 123458},
		{"carry into next second", 999999789, 1000000},
		{"exact microsecond", 123456000, 123456},
	}
	base := time.Date(2026, 11, 17, 12, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := healthTestProxy()
			builder.UpdatedAt = base.Add(time.Duration(tt.nanoseconds))
			fromDatabase := *builder
			fromDatabase.UpdatedAt = base.UTC().Add(time.Duration(tt.storedMicroseconds) * time.Microsecond)
			require.Equal(t, ProxyProbeConfigVersion(builder), ProxyProbeConfigVersion(&fromDatabase), "a cache fence created from Ent's builder timestamp must accept a later DB read")
		})
	}
}

func TestProxyHealthCacheExpiresColumnsIndependently(t *testing.T) {
	p := healthTestProxy()
	info := newProxyProbeSnapshot(p)
	fresh, old := time.Now().Add(-30*time.Minute), time.Now().Add(-61*time.Minute)
	info.NodeStatus, info.BusinessStatus, info.RouteStatus = ProxyHealthHealthy, ProxyHealthHealthy, ProxyHealthHealthy
	info.NodeCheckedAt, info.BusinessCheckedAt, info.RouteCheckedAt = &fresh, &fresh, &old
	info.PreferredNode, info.IsBackup = "us2", true
	cache := &healthMemoryCache{value: info}
	s := &adminServiceImpl{proxyLatencyCache: cache}
	rows := []ProxyWithAccountCount{{Proxy: *p}}
	s.attachProxyLatency(context.Background(), rows)
	require.Equal(t, ProxyHealthHealthy, rows[0].NodeStatus)
	require.Equal(t, ProxyHealthHealthy, rows[0].BusinessStatus)
	require.Equal(t, ProxyHealthUnknown, rows[0].RouteStatus)
	require.Empty(t, rows[0].PreferredNode)
	require.Equal(t, "us2", cache.value.PreferredNode, "list expiry must not mutate the cached sample")
}

func TestProxyQualitySnapshotPreservesThreeColumnSample(t *testing.T) {
	p := healthTestProxy()
	info := newProxyProbeSnapshot(p)
	info.NodeStatus, info.BusinessStatus, info.PreferredNode = ProxyHealthUnhealthy, ProxyHealthHealthy, "us2"
	info.RouteStatus, info.IsBackup = ProxyHealthHealthy, true
	cache := &healthMemoryCache{value: info}
	s := &adminServiceImpl{proxyRepo: &healthCurrentProxyRepo{proxy: p}, proxyLatencyCache: cache}
	s.saveProxyQualitySnapshot(context.Background(), p, &ProxyQualityCheckResult{Score: 90, Grade: "A", CheckedAt: time.Now().Unix()}, &ProxyExitInfo{IP: "192.0.2.1"})
	require.Equal(t, ProxyHealthUnhealthy, cache.value.NodeStatus)
	require.Equal(t, ProxyHealthHealthy, cache.value.BusinessStatus)
	require.Equal(t, "us2", cache.value.PreferredNode)
	require.Equal(t, info.SampleID, cache.value.SampleID)
	require.Equal(t, info.NodeCheckedAt, cache.value.NodeCheckedAt)
	require.Equal(t, "192.0.2.1", cache.value.IPAddress)
}
