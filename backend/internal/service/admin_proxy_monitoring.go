package service

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// ProxyTaskConfigCoordinator serializes proxy edits with scheduled detection and
// mail delivery. Keeping this optional preserves the existing admin constructor.
type ProxyTaskConfigCoordinator interface {
	WithProxyConfigLock(context.Context, int64, func(context.Context) error) error
	ResetProxyTargets(context.Context, int64) error
}

func ConfigureProxyTaskCoordination(admin AdminService, coordinator ProxyTaskConfigCoordinator) {
	if implementation, ok := admin.(*adminServiceImpl); ok {
		implementation.proxyTaskCoordinator = coordinator
	}
}

func (s *adminServiceImpl) UpdateProxy(ctx context.Context, id int64, input *UpdateProxyInput) (*Proxy, error) {
	var result *Proxy
	update := func(locked context.Context) error {
		var err error
		result, err = s.updateProxy(locked, id, input)
		return err
	}
	if s.proxyTaskCoordinator != nil {
		err := s.proxyTaskCoordinator.WithProxyConfigLock(ctx, id, update)
		return result, err
	}
	err := update(ctx)
	return result, err
}

func (s *adminServiceImpl) DeleteProxy(ctx context.Context, id int64) error {
	if s.proxyTaskCoordinator != nil {
		return s.proxyTaskCoordinator.WithProxyConfigLock(ctx, id, func(locked context.Context) error {
			return s.deleteProxy(locked, id)
		})
	}
	return s.deleteProxy(ctx, id)
}

func (s *adminServiceImpl) invalidateProxyMonitoring(ctx context.Context, previous, current *Proxy) error {
	// A cosmetic edit does not start a new node-failure notification cycle.
	before, after := *previous, *current
	before.UpdatedAt, after.UpdatedAt = time.Time{}, time.Time{}
	changed := ProxyProbeConfigVersion(&before) != ProxyProbeConfigVersion(&after)
	if changed && s.proxyTaskCoordinator != nil {
		if err := s.proxyTaskCoordinator.ResetProxyTargets(ctx, current.ID); err != nil {
			return err
		}
	}
	if invalidator, ok := s.proxyLatencyCache.(ProxyLatencyInvalidator); ok {
		if err := invalidator.InvalidateProxyLatency(ctx, current.ID, ProxyProbeConfigVersion(current)); err != nil {
			// List reads validate the version against the current DB proxy as well.
			logger.LegacyPrintf("service.admin_proxy", "invalidate proxy monitoring cache %d: %v", current.ID, err)
		}
	}
	return nil
}
