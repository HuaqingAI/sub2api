package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
	opts := defaultProxyHealthProbeOptions()
	opts.controllerURL = controller.URL
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
	p.MihomoGroup = "us2-fallback"
	require.Error(t, ValidateProxyNodeTestConfig(p))
	p = healthTestProxy()
	p.Host = "remote.example"
	require.Error(t, ValidateProxyNodeTestConfig(p))
	p = healthTestProxy()
	p.Port = 7905
	require.Error(t, ValidateProxyNodeTestConfig(p))
	p = &Proxy{Protocol: "https", Host: "example.com", Port: 443}
	require.NoError(t, ValidateProxyNodeTestConfig(p))
	require.Equal(t, p.URL(), p.NodeTestURL())
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
	fresh, old := time.Now(), time.Now().Add(-16*time.Minute)
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
