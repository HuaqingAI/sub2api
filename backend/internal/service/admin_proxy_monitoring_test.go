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
	port, node, group := 28006, " us6 primary ", " GPT exits west "

	businessPort := repo.proxy.Port
	_, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{NodeTestPort: &businessPort})
	require.Error(t, err, "the fallback business listener cannot serve as the original-node test listener")
	require.Zero(t, repo.updateCalls)
	require.Empty(t, coordinator.resets)

	proxy, err := svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{
		Port: 8123, NodeTestPort: &port, MihomoPrimaryNode: &node, MihomoGroup: &group,
	})
	require.NoError(t, err)
	require.Equal(t, "socks5://127.0.0.1:8123", proxy.URL())
	require.Equal(t, "socks5h://127.0.0.1:28006", proxy.NodeTestURL())
	require.Equal(t, "us6 primary", proxy.MihomoPrimaryNode)
	require.Equal(t, "GPT exits west", proxy.MihomoGroup)
	require.Equal(t, []int64{9}, coordinator.resets)

	// Changing only the raw listener port preserves the custom node/group names
	// and starts a fresh notification cycle for the new detection configuration.
	newPort := 28007
	proxy, err = svc.UpdateProxy(context.Background(), 9, &UpdateProxyInput{NodeTestPort: &newPort})
	require.NoError(t, err)
	require.Equal(t, "socks5h://127.0.0.1:28007", proxy.NodeTestURL())
	require.Equal(t, "us6 primary", proxy.MihomoPrimaryNode)
	require.Equal(t, "GPT exits west", proxy.MihomoGroup)
	require.Equal(t, []int64{9, 9}, coordinator.resets)
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

type monitoringCreateRepo struct {
	ProxyRepository
	created *Proxy
	err     error
}

func (r *monitoringCreateRepo) Create(_ context.Context, p *Proxy) error {
	copy := *p
	r.created = &copy
	return r.err
}

func TestAdminMonitoringCreateAcceptsCustomMetadataBeforePersistence(t *testing.T) {
	// Stop at the persistence boundary so this test cannot launch background
	// probes against a real local sidecar.
	writeErr := errors.New("persistence unavailable")
	repo := &monitoringCreateRepo{err: writeErr}
	svc := &adminServiceImpl{proxyRepo: repo}
	port := 28006
	_, err := svc.CreateProxy(context.Background(), &CreateProxyInput{
		Name: "Sixth US exit", Protocol: "socks5", Host: "127.0.0.1", Port: 8123,
		NodeTestPort: &port, MihomoPrimaryNode: "  US 6 原节点  ", MihomoGroup: "  Custom US pool  ",
	})
	require.ErrorIs(t, err, writeErr, "custom bindings must pass validation and reach persistence")
	require.NotNil(t, repo.created)
	require.Equal(t, "US 6 原节点", repo.created.MihomoPrimaryNode)
	require.Equal(t, "Custom US pool", repo.created.MihomoGroup)
	require.Equal(t, "socks5h://127.0.0.1:28006", repo.created.NodeTestURL())
	require.Equal(t, "socks5://127.0.0.1:8123", repo.created.URL())

	repo.created = nil
	_, err = svc.CreateProxy(context.Background(), &CreateProxyInput{
		Name: "Invalid detector", Protocol: "socks5", Host: "127.0.0.1", Port: 28006,
		NodeTestPort: &port, MihomoPrimaryNode: "us6", MihomoGroup: "Custom US pool",
	})
	require.ErrorContains(t, err, "must differ from the business proxy port")
	require.Nil(t, repo.created, "invalid bindings must be rejected before persistence")
}
