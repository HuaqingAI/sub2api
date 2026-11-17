package service

import (
	"context"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

// ProxyTestTaskRunnerService scans persisted schedules; task session locks elect the executing Pod.
type ProxyTestTaskRunnerService struct {
	service   *ProxyTestTaskService
	cancel    context.CancelFunc
	wg        sync.WaitGroup
	startOnce sync.Once
	stopOnce  sync.Once
}

func NewProxyTestTaskRunnerService(service *ProxyTestTaskService) *ProxyTestTaskRunnerService {
	return &ProxyTestTaskRunnerService{service: service}
}

func (r *ProxyTestTaskRunnerService) Start() {
	if r == nil || r.service == nil {
		return
	}
	r.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(context.Background())
		r.cancel = cancel
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			// Run once at startup so a restart resumes overdue tasks without waiting a full minute.
			r.scan(ctx)
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					r.scan(ctx)
				}
			}
		}()
	})
}

func (r *ProxyTestTaskRunnerService) Stop() {
	if r == nil {
		return
	}
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
		}
		r.wg.Wait()
	})
}

func (r *ProxyTestTaskRunnerService) scan(ctx context.Context) {
	queryCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	ids, err := r.service.repo.ListDue(queryCtx, r.service.now())
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			logger.LegacyPrintf("service.proxy_test_task_runner", "list due tasks: %v", err)
		}
		return
	}
	sem := make(chan struct{}, 2)
	var wg sync.WaitGroup
	for _, id := range ids {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			defer func() { <-sem }()
			if _, err := r.service.run(ctx, id, true); err != nil && ctx.Err() == nil {
				logger.LegacyPrintf("service.proxy_test_task_runner", "task %d: %v", id, err)
			}
		}(id)
	}
	wg.Wait()
}
