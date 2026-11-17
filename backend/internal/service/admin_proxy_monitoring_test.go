//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type monitoringCoordinatorStub struct {
	locked bool
	resets []int64
	err    error
}

func (c *monitoringCoordinatorStub) WithProxyConfigLock(ctx context.Context, _ int64, fn func(context.Context) error) error {
	if c.err != nil {
		return c.err
	}
	c.locked = true
	defer func() { c.locked = false }()
	return fn(ctx)
}

func (c *monitoringCoordinatorStub) ResetProxyTargets(_ context.Context, id int64) error {
	if !c.locked {
		return errors.New("notification cycle reset outside the task lock")
	}
	c.resets = append(c.resets, id)
	return nil
}

func monitoringProxyForUpdate() *Proxy {
	port := 17903
	return &Proxy{ID: 9, Name: "US 1", Protocol: "socks5", Host: "127.0.0.1", Port: 7903,
		Status: StatusActive, NodeTestPort: &port, MihomoPrimaryNode: "us1", MihomoGroup: "us1-fallback"}
}

func TestAdminMonitoringConfigPartialUpdateAndClear(t *testing.T) {
	repo := &updatingProxyRepoStub{proxyRepoStub: &proxyRepoStub{}, proxy: monitoringProxyForUpdate()}
	coordinator := &monitoringCoordinatorStub{}
	svc := &adminServiceImpl{proxyRepo: repo, proxyTaskCoordinator: coordinator}

	proxy, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{Name: "Renamed"})
	require.NoError(t, err)
	require.Equal(t, "us1", proxy.MihomoPrimaryNode)
	require.Equal(t, 17903, *proxy.NodeTestPort)
	require.Empty(t, coordinator.resets, "a name edit must not reopen the fault notification cycle")

	proxy, err = svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{ClearNodeTest: true})
	require.NoError(t, err)
	require.Nil(t, proxy.NodeTestPort)
	require.Empty(t, proxy.MihomoPrimaryNode)
	require.Empty(t, proxy.MihomoGroup)
	require.Equal(t, []int64{9}, coordinator.resets)
}

func TestAdminMonitoringConfigSwitchIsValidatedAndResetsCycle(t *testing.T) {
	repo := &updatingProxyRepoStub{proxyRepoStub: &proxyRepoStub{}, proxy: monitoringProxyForUpdate()}
	coordinator := &monitoringCoordinatorStub{}
	svc := &adminServiceImpl{proxyRepo: repo, proxyTaskCoordinator: coordinator}
	port, node, group := 17905, "us2", "us2-fallback"

	_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{NodeTestPort: &port})
	require.Error(t, err, "raw us2 must not silently test a business entry for us1")
	require.Zero(t, repo.updateCalls)
	require.Empty(t, coordinator.resets)

	proxy, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{
		Port: 7905, NodeTestPort: &port, MihomoPrimaryNode: &node, MihomoGroup: &group,
	})
	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:7905", proxy.URL())
	require.Equal(t, "socks5h://127.0.0.1:17905", proxy.NodeTestURL())
	require.Equal(t, []int64{9}, coordinator.resets)
}

func TestAdminMonitoringConfigLockFailurePreventsMutation(t *testing.T) {
	repo := &updatingProxyRepoStub{proxyRepoStub: &proxyRepoStub{}, proxy: monitoringProxyForUpdate()}
	coordinator := &monitoringCoordinatorStub{err: errors.New("task is running")}
	svc := &adminServiceImpl{proxyRepo: repo, proxyTaskCoordinator: coordinator}
	_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{ClearNodeTest: true})
	require.Error(t, err)
	require.Zero(t, repo.updateCalls)
	require.NotNil(t, repo.proxy.NodeTestPort)
}
