package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestProxyHealthCacheAtomicOrderingAndConfigurationFence(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := &proxyLatencyCache{rdb: rdb}
	ctx := context.Background()
	old := &service.ProxyLatencyInfo{NodeStatus: service.ProxyHealthUnhealthy, ProbeConfigVersion: "00001-old", SampleID: "00001", UpdatedAt: time.Now()}
	require.NoError(t, cache.SetProxyLatency(ctx, 1, old))
	newSample := *old
	newSample.SampleID = "00002"
	newSample.NodeStatus = service.ProxyHealthHealthy
	require.NoError(t, cache.SetProxyLatency(ctx, 1, &newSample))
	require.NoError(t, cache.SetProxyLatency(ctx, 1, old))
	values, err := cache.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, service.ProxyHealthHealthy, values[1].NodeStatus)
	require.Equal(t, "00002", values[1].SampleID)

	require.NoError(t, cache.InvalidateProxyLatency(ctx, 1, "00002-new"))
	require.NoError(t, cache.SetProxyLatency(ctx, 1, &newSample))
	values, err = cache.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Nil(t, values[1], "old configuration must not repopulate the cache after invalidation")

	newVersion := newSample
	newVersion.ProbeConfigVersion = "00002-new"
	newVersion.SampleID = "00003"
	require.NoError(t, cache.SetProxyLatency(ctx, 1, &newVersion))
	require.NoError(t, cache.InvalidateProxyLatency(ctx, 1, "00001-old"))
	values, err = cache.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, "00002-new", values[1].ProbeConfigVersion, "a delayed invalidation cannot delete a newer version")
}

func TestProxyHealthCachePreservesQualityButDoesNotReviveOldIP(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := &proxyLatencyCache{rdb: rdb}
	ctx := context.Background()
	checked := time.Now().Unix()
	old := &service.ProxyLatencyInfo{ProbeConfigVersion: "v1", SampleID: "00001", IPAddress: "192.0.2.1", QualityGrade: "A", QualityCheckedAt: &checked}
	require.NoError(t, cache.SetProxyLatency(ctx, 1, old))
	current := &service.ProxyLatencyInfo{ProbeConfigVersion: "v1", SampleID: "00002", NodeStatus: service.ProxyHealthHealthy}
	require.NoError(t, cache.SetProxyLatency(ctx, 1, current))
	values, err := cache.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, "A", values[1].QualityGrade)
	require.Empty(t, values[1].IPAddress, "fresh HEAD cannot make an older exit-IP observation appear current")
}

func TestProxyHealthQualityOnlyWriteAtomicallyPreservesLatestHealthRound(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	cache := &proxyLatencyCache{rdb: rdb}
	ctx := context.Background()
	checked := time.Now().Unix()
	health := &service.ProxyLatencyInfo{ProbeConfigVersion: "v1", SampleID: "00002", NodeStatus: service.ProxyHealthUnhealthy, BusinessStatus: service.ProxyHealthHealthy, PreferredNode: "us2", SampledPod: "pod-a"}
	require.NoError(t, cache.SetProxyLatency(ctx, 1, health))
	quality := &service.ProxyLatencyInfo{QualityOnly: true, ProbeConfigVersion: "v1", SampleID: "00003", NodeStatus: service.ProxyHealthUnknown, QualityGrade: "A", QualityCheckedAt: &checked, IPAddress: "192.0.2.1"}
	require.NoError(t, cache.SetProxyLatency(ctx, 1, quality))
	values, err := cache.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, service.ProxyHealthUnhealthy, values[1].NodeStatus)
	require.Equal(t, service.ProxyHealthHealthy, values[1].BusinessStatus)
	require.Equal(t, "us2", values[1].PreferredNode)
	require.Equal(t, "pod-a", values[1].SampledPod)
	require.Equal(t, "00002", values[1].SampleID)
	require.Equal(t, "A", values[1].QualityGrade)
	require.Equal(t, "192.0.2.1", values[1].IPAddress)
	quality.SampleID, quality.IPAddress = "00001", ""
	require.NoError(t, cache.SetProxyLatency(ctx, 1, quality))
	values, err = cache.GetProxyLatencies(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, "00002", values[1].SampleID, "a quality write can never replace the sampling identity")
	require.Empty(t, values[1].IPAddress, "a failed IP lookup must not extend an earlier IP observation")
}
