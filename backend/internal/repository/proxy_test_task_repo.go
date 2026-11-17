package repository

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type proxyTestTaskRepository struct{ db *sql.DB }

func NewProxyTestTaskRepository(db *sql.DB) service.ProxyTestTaskRepository {
	return &proxyTestTaskRepository{db: db}
}

type proxyTaskSessionKey struct{}
type proxyTaskSession struct {
	repo *proxyTestTaskRepository
	conn *sql.Conn
}
type proxyTaskSQL interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *proxyTestTaskRepository) session(ctx context.Context) *sql.Conn {
	if s, ok := ctx.Value(proxyTaskSessionKey{}).(*proxyTaskSession); ok && s.repo == r {
		return s.conn
	}
	return nil
}

func (r *proxyTestTaskRepository) query(ctx context.Context) proxyTaskSQL {
	if conn := r.session(ctx); conn != nil {
		return conn
	}
	return r.db
}

func (r *proxyTestTaskRepository) begin(ctx context.Context) (*sql.Tx, error) {
	if conn := r.session(ctx); conn != nil {
		return conn.BeginTx(ctx, nil)
	}
	return r.db.BeginTx(ctx, nil)
}

const proxyTaskColumns = `id, name, cron_expression, timezone, enabled, recipients, failure_threshold,
 timeout_seconds, last_run_at, next_run_at, last_run_status, last_error, config_version, created_at, updated_at`

func (r *proxyTestTaskRepository) Get(ctx context.Context, id int64) (*service.ProxyTestTask, error) {
	task := &service.ProxyTestTask{ProxyIDs: []int64{}, Recipients: []string{}, Targets: []*service.ProxyTestTaskTarget{}}
	var recipients []byte
	err := r.query(ctx).QueryRowContext(ctx, `SELECT `+proxyTaskColumns+` FROM proxy_test_tasks WHERE id=$1`, id).Scan(
		&task.ID, &task.Name, &task.CronExpression, &task.Timezone, &task.Enabled, &recipients, &task.FailureThreshold,
		&task.TimeoutSeconds, &task.LastRunAt, &task.NextRunAt, &task.LastRunStatus, &task.LastError, &task.ConfigVersion,
		&task.CreatedAt, &task.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, infraerrors.NotFound("PROXY_TEST_TASK_NOT_FOUND", "proxy test task not found")
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(recipients, &task.Recipients); err != nil {
		return nil, fmt.Errorf("decode task recipients: %w", err)
	}
	rows, err := r.query(ctx).QueryContext(ctx, `SELECT t.task_id,t.proxy_id,p.name,COALESCE(p.mihomo_primary_node,''),
 t.last_node_status,t.last_checked_at,t.last_error,t.consecutive_failures,t.failure_started_at,t.notified_recipients,t.last_probe_config_version,t.last_result
 FROM proxy_test_task_targets t JOIN proxies p ON p.id=t.proxy_id WHERE t.task_id=$1 ORDER BY t.proxy_id`, id)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		target := &service.ProxyTestTaskTarget{NotifiedRecipients: []string{}}
		var notified, result []byte
		if err := rows.Scan(&target.TaskID, &target.ProxyID, &target.ProxyName, &target.MihomoPrimaryNode,
			&target.LastNodeStatus, &target.LastCheckedAt, &target.LastError, &target.ConsecutiveFailures,
			&target.FailureStartedAt, &notified, &target.LastProbeConfigVersion, &result); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(notified, &target.NotifiedRecipients); err != nil {
			return nil, err
		}
		if len(result) > 0 {
			if err := json.Unmarshal(result, &target.LastResult); err != nil {
				return nil, err
			}
		}
		task.ProxyIDs = append(task.ProxyIDs, target.ProxyID)
		task.Targets = append(task.Targets, target)
	}
	return task, rows.Err()
}

func (r *proxyTestTaskRepository) List(ctx context.Context) ([]*service.ProxyTestTask, error) {
	rows, err := r.query(ctx).QueryContext(ctx, `SELECT id FROM proxy_test_tasks ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]*service.ProxyTestTask, 0, len(ids))
	for _, id := range ids {
		task, err := r.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, nil
}

func (r *proxyTestTaskRepository) ListDue(ctx context.Context, now time.Time) ([]int64, error) {
	rows, err := r.query(ctx).QueryContext(ctx, `SELECT id FROM proxy_test_tasks WHERE enabled=TRUE AND next_run_at <= $1 ORDER BY next_run_at LIMIT 20`, now)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Save is called under the association/task lock. Proxy row locks also protect the
// cross-task uniqueness check against other application writers using this contract.
func (r *proxyTestTaskRepository) Save(ctx context.Context, task *service.ProxyTestTask) (*service.ProxyTestTask, error) {
	tx, err := r.begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM proxies WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE`, pq.Array(task.ProxyIDs))
	if err != nil {
		return nil, err
	}
	count := 0
	for rows.Next() {
		count++
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if count != len(task.ProxyIDs) {
		return nil, infraerrors.BadRequest("PROXY_TEST_TARGET_NOT_FOUND", "one or more selected proxies no longer exist")
	}
	if task.Enabled {
		var conflict bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM proxy_test_task_targets x JOIN proxy_test_tasks t ON t.id=x.task_id
 WHERE x.proxy_id=ANY($1) AND t.enabled=TRUE AND t.id<>$2)`, pq.Array(task.ProxyIDs), task.ID).Scan(&conflict); err != nil {
			return nil, err
		}
		if conflict {
			return nil, infraerrors.Conflict("PROXY_TEST_TARGET_IN_USE", "a selected proxy already belongs to an enabled test task")
		}
	}
	recipients, err := json.Marshal(task.Recipients)
	if err != nil {
		return nil, err
	}
	if task.ID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO proxy_test_tasks(name,cron_expression,timezone,enabled,recipients,failure_threshold,timeout_seconds,next_run_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`, task.Name, task.CronExpression, task.Timezone, task.Enabled,
			string(recipients), task.FailureThreshold, task.TimeoutSeconds, task.NextRunAt).Scan(&task.ID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE proxy_test_tasks SET name=$2,cron_expression=$3,timezone=$4,enabled=$5,recipients=$6,
 failure_threshold=$7,timeout_seconds=$8,next_run_at=$9,config_version=config_version+1,updated_at=NOW() WHERE id=$1`,
			task.ID, task.Name, task.CronExpression, task.Timezone, task.Enabled, string(recipients), task.FailureThreshold, task.TimeoutSeconds, task.NextRunAt)
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM proxy_test_task_targets WHERE task_id=$1 AND NOT(proxy_id=ANY($2))`, task.ID, pq.Array(task.ProxyIDs)); err != nil {
		return nil, err
	}
	for _, id := range task.ProxyIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO proxy_test_task_targets(task_id,proxy_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, task.ID, id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, task.ID)
}

func (r *proxyTestTaskRepository) Delete(ctx context.Context, id int64) error {
	_, err := r.query(ctx).ExecContext(ctx, `DELETE FROM proxy_test_tasks WHERE id=$1`, id)
	return err
}

func (r *proxyTestTaskRepository) SaveRun(ctx context.Context, task *service.ProxyTestTask, scheduled bool) error {
	tx, err := r.begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	query := `UPDATE proxy_test_tasks SET last_run_at=$2,last_run_status=$3,last_error=$4 WHERE id=$1`
	args := []any{task.ID, task.LastRunAt, task.LastRunStatus, task.LastError}
	if scheduled {
		query += ` AND config_version=$5`
		args = append(args, task.ConfigVersion)
	}
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	if scheduled {
		if _, err := tx.ExecContext(ctx, `UPDATE proxy_test_tasks SET next_run_at=$2 WHERE id=$1 AND config_version=$3`, task.ID, task.NextRunAt, task.ConfigVersion); err != nil {
			return err
		}
	}
	for _, target := range task.Targets {
		result, err := json.Marshal(target.LastResult)
		if err != nil {
			return err
		}
		if scheduled {
			notified, err := json.Marshal(target.NotifiedRecipients)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, `UPDATE proxy_test_task_targets SET last_node_status=$3,last_checked_at=$4,last_error=$5,
 consecutive_failures=$6,failure_started_at=$7,notified_recipients=$8,last_result=$9,last_probe_config_version=$10 WHERE task_id=$1 AND proxy_id=$2`,
				task.ID, target.ProxyID, target.LastNodeStatus, target.LastCheckedAt, target.LastError, target.ConsecutiveFailures,
				target.FailureStartedAt, string(notified), string(result), target.LastProbeConfigVersion)
			if err != nil {
				return err
			}
		} else {
			if _, err := tx.ExecContext(ctx, `UPDATE proxy_test_task_targets SET last_node_status=$3,last_checked_at=$4,last_error=$5,last_result=$6 WHERE task_id=$1 AND proxy_id=$2`,
				task.ID, target.ProxyID, target.LastNodeStatus, target.LastCheckedAt, target.LastError, string(result)); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (r *proxyTestTaskRepository) ResetProxyTargets(ctx context.Context, proxyID int64) error {
	_, err := r.query(ctx).ExecContext(ctx, `UPDATE proxy_test_task_targets SET last_node_status='unknown',last_checked_at=NULL,last_error='',
 consecutive_failures=0,failure_started_at=NULL,notified_recipients='[]'::jsonb,last_probe_config_version='',last_result=NULL WHERE proxy_id=$1`, proxyID)
	return err
}

const proxyTaskAssociationLock = "sub2api:proxy-test:associations"

func proxyTaskLockName(id int64) string {
	return "sub2api:proxy-test:task:" + strconv.FormatInt(id, 10)
}

func (r *proxyTestTaskRepository) withSession(ctx context.Context, fn func(context.Context, *sql.Conn) error) error {
	conn, err := r.db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// A canceled probe context must not return a session holding advisory locks to the pool.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, `SELECT pg_advisory_unlock_all()`); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}()
	return fn(context.WithValue(ctx, proxyTaskSessionKey{}, &proxyTaskSession{repo: r, conn: conn}), conn)
}

func acquireProxyTaskLock(ctx context.Context, conn *sql.Conn, name string) error {
	_, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock(hashtextextended($1, 0))`, name)
	return err
}

func (r *proxyTestTaskRepository) WithTaskLock(ctx context.Context, id int64, edit bool, fn func(context.Context) error) error {
	return r.withSession(ctx, func(locked context.Context, conn *sql.Conn) error {
		if edit {
			if err := acquireProxyTaskLock(locked, conn, proxyTaskAssociationLock); err != nil {
				return err
			}
		}
		if id > 0 {
			if err := acquireProxyTaskLock(locked, conn, proxyTaskLockName(id)); err != nil {
				return err
			}
		}
		return fn(locked)
	})
}

func (r *proxyTestTaskRepository) WithProxyConfigLock(ctx context.Context, proxyID int64, fn func(context.Context) error) error {
	return r.withSession(ctx, func(locked context.Context, conn *sql.Conn) error {
		if err := acquireProxyTaskLock(locked, conn, proxyTaskAssociationLock); err != nil {
			return err
		}
		rows, err := conn.QueryContext(locked, `SELECT task_id FROM proxy_test_task_targets WHERE proxy_id=$1 ORDER BY task_id`, proxyID)
		if err != nil {
			return err
		}
		ids := []int64{}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			if err := acquireProxyTaskLock(locked, conn, proxyTaskLockName(id)); err != nil {
				return err
			}
		}
		return fn(locked)
	})
}
