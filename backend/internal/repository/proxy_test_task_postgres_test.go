package repository

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// An explicit isolated test DSN is required. Each test owns a temporary schema.
func proxyTaskPostgresDB(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("PROXY_TASK_TEST_DSN")
	if dsn == "" {
		t.Skip("requires isolated PROXY_TASK_TEST_DSN")
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		var err error
		dsn, err = pq.ParseURL(dsn)
		require.NoError(t, err)
	}
	base, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	schema := fmt.Sprintf("proxy_task_test_%d", time.Now().UnixNano())
	_, err = base.Exec(`CREATE SCHEMA ` + schema)
	require.NoError(t, err)
	db, err := sql.Open("postgres", dsn+" search_path="+schema)
	require.NoError(t, err)
	db.SetMaxOpenConns(10)
	t.Cleanup(func() {
		_ = db.Close()
		_, _ = base.Exec(`DROP SCHEMA ` + schema + ` CASCADE`)
		_ = base.Close()
	})
	_, err = db.Exec(`CREATE TABLE proxies(id BIGINT PRIMARY KEY, name TEXT NOT NULL, deleted_at TIMESTAMPTZ);
 INSERT INTO proxies(id,name) VALUES(10,'us1'),(11,'us2'),(12,'us3')`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "251_proxy_test_tasks.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err, "migration must be idempotent")
	return db
}

func proxyTaskPostgresFixture(id int64) *service.ProxyTestTask {
	next := time.Now().Add(-time.Minute)
	return &service.ProxyTestTask{Name: "US primary checks", ProxyIDs: []int64{id}, CronExpression: "*/5 * * * *", Timezone: "Asia/Shanghai",
		Enabled: true, Recipients: []string{"a@test.example"}, FailureThreshold: 2, TimeoutSeconds: 10, NextRunAt: &next}
}

func saveProxyTaskPostgres(ctx context.Context, repo service.ProxyTestTaskRepository, task *service.ProxyTestTask) (*service.ProxyTestTask, error) {
	var saved *service.ProxyTestTask
	err := repo.WithTaskLock(ctx, task.ID, true, func(locked context.Context) error {
		var err error
		saved, err = repo.Save(locked, task)
		return err
	})
	return saved, err
}

func TestProxyTaskPostgresMigrationCRUDAndManualCycleIsolation(t *testing.T) {
	db := proxyTaskPostgresDB(t)
	repo := NewProxyTestTaskRepository(db)
	ctx := context.Background()
	task, err := saveProxyTaskPostgres(ctx, repo, proxyTaskPostgresFixture(10))
	require.NoError(t, err)
	require.Equal(t, []int64{10}, task.ProxyIDs)
	require.Equal(t, int64(1), task.ConfigVersion)
	require.Len(t, task.Targets, 1)
	ids, err := repo.ListDue(ctx, time.Now())
	require.NoError(t, err)
	require.Equal(t, []int64{task.ID}, ids)
	err = repo.WithTaskLock(ctx, task.ID, false, func(locked context.Context) error {
		now := time.Now()
		task.LastRunAt, task.LastRunStatus = &now, "success"
		target := task.Targets[0]
		target.LastNodeStatus, target.LastCheckedAt, target.ConsecutiveFailures = "unhealthy", &now, 2
		target.FailureStartedAt, target.NotifiedRecipients = &now, []string{"a@test.example"}
		target.LastProbeConfigVersion = "00000000000000000001-identity"
		target.LastResult = &service.ProxyTestResult{NodeStatus: "unhealthy", BusinessStatus: "healthy", PreferredNode: "us2", IsBackup: true}
		return repo.SaveRun(locked, task, true)
	})
	require.NoError(t, err)
	task, err = repo.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "us2", task.Targets[0].LastResult.PreferredNode)
	require.Equal(t, "00000000000000000001-identity", task.Targets[0].LastProbeConfigVersion)
	due := *task.NextRunAt
	err = repo.WithTaskLock(ctx, task.ID, false, func(locked context.Context) error {
		task.LastRunStatus = "manual"
		target := task.Targets[0]
		target.LastNodeStatus, target.ConsecutiveFailures, target.NotifiedRecipients, target.LastProbeConfigVersion = "healthy", 0, []string{}, "manual-identity"
		return repo.SaveRun(locked, task, false)
	})
	require.NoError(t, err)
	task, err = repo.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Equal(t, "healthy", task.Targets[0].LastNodeStatus)
	require.Equal(t, 2, task.Targets[0].ConsecutiveFailures)
	require.Equal(t, []string{"a@test.example"}, task.Targets[0].NotifiedRecipients)
	require.Equal(t, "00000000000000000001-identity", task.Targets[0].LastProbeConfigVersion)
	require.True(t, due.Equal(*task.NextRunAt))
	task.Name, task.ProxyIDs = "Updated", []int64{10, 11}
	task, err = saveProxyTaskPostgres(ctx, repo, task)
	require.NoError(t, err)
	require.Equal(t, int64(2), task.ConfigVersion)
	require.Len(t, task.Targets, 2)
	err = repo.WithProxyConfigLock(ctx, 10, func(locked context.Context) error { return repo.ResetProxyTargets(locked, 10) })
	require.NoError(t, err)
	task, err = repo.Get(ctx, task.ID)
	require.NoError(t, err)
	require.Zero(t, task.Targets[0].ConsecutiveFailures)
	require.Empty(t, task.Targets[0].LastProbeConfigVersion)
	err = repo.WithTaskLock(ctx, task.ID, true, func(locked context.Context) error { return repo.Delete(locked, task.ID) })
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRow(`SELECT count(*) FROM proxy_test_task_targets`).Scan(&count))
	require.Zero(t, count)
}

func TestProxyTaskPostgresConcurrentEnabledTaskUniqueness(t *testing.T) {
	db := proxyTaskPostgresDB(t)
	repo := NewProxyTestTaskRepository(db)
	var wg sync.WaitGroup
	errors := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := saveProxyTaskPostgres(context.Background(), repo, proxyTaskPostgresFixture(10))
			errors <- err
		}()
	}
	wg.Wait()
	close(errors)
	succeeded, failed := 0, 0
	for err := range errors {
		if err == nil {
			succeeded++
		} else {
			failed++
		}
	}
	require.Equal(t, 1, succeeded)
	require.Equal(t, 1, failed)
	draft := proxyTaskPostgresFixture(10)
	draft.Enabled = false
	_, err := saveProxyTaskPostgres(context.Background(), repo, draft)
	require.NoError(t, err, "disabled drafts can include the same proxy")
}

func TestProxyTaskPostgresConfigEditWaitsForExecutionAndCanceledLockIsReleased(t *testing.T) {
	db := proxyTaskPostgresDB(t)
	repo := NewProxyTestTaskRepository(db)
	ctx := context.Background()
	task, err := saveProxyTaskPostgres(ctx, repo, proxyTaskPostgresFixture(10))
	require.NoError(t, err)
	entered, release := make(chan struct{}), make(chan struct{})
	runDone := make(chan error, 1)
	go func() {
		runDone <- repo.WithTaskLock(ctx, task.ID, false, func(context.Context) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	editEntered, editDone := make(chan struct{}), make(chan error, 1)
	go func() {
		editDone <- repo.WithProxyConfigLock(ctx, 10, func(context.Context) error { close(editEntered); return nil })
	}()
	select {
	case <-editEntered:
		t.Fatal("proxy config edit entered while the old node was being probed/emailed")
	case <-time.After(100 * time.Millisecond):
	}
	blockedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	err = repo.WithTaskLock(blockedCtx, task.ID, false, func(context.Context) error { return nil })
	require.Error(t, err)
	close(release)
	require.NoError(t, <-runDone)
	require.NoError(t, <-editDone)
	require.NoError(t, repo.WithTaskLock(ctx, task.ID, true, func(context.Context) error { return nil }), "canceled acquisition must not leak any lock")
}
