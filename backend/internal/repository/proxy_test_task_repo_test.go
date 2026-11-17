package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestProxyTaskConfigMutationLocksAssociationsThenAllTasks(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	db.SetMaxOpenConns(1)
	repo := NewProxyTestTaskRepository(db)
	lockSQL := regexp.QuoteMeta(`SELECT pg_advisory_lock(hashtextextended($1, 0))`)
	mock.ExpectExec(lockSQL).WithArgs(proxyTaskAssociationLock).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT task_id FROM proxy_test_task_targets WHERE proxy_id=$1 ORDER BY task_id`)).WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"task_id"}).AddRow(int64(3)).AddRow(int64(8)))
	mock.ExpectExec(lockSQL).WithArgs(proxyTaskLockName(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(lockSQL).WithArgs(proxyTaskLockName(8)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE proxy_test_task_targets SET").WithArgs(int64(10)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_unlock_all()`)).WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.WithProxyConfigLock(context.Background(), 10, func(ctx context.Context) error {
		// The one-connection pool also verifies that callback operations reuse the lock session.
		return repo.ResetProxyTargets(ctx, 10)
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProxyTaskRunLockDoesNotAcquireAssociationLock(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewProxyTestTaskRepository(db)
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_lock(hashtextextended($1, 0))`)).
		WithArgs(proxyTaskLockName(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`SELECT pg_advisory_unlock_all()`)).WillReturnResult(sqlmock.NewResult(0, 1))
	ctx, cancel := context.WithCancel(context.Background())
	if err := repo.WithTaskLock(ctx, 3, false, func(context.Context) error {
		cancel() // Cleanup must use a fresh context to avoid leaking a session lock into the pool.
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProxyTaskSaveRejectsProxyAlreadyInAnotherEnabledTask(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewProxyTestTaskRepository(db)
	mock.ExpectBegin()
	mock.ExpectQuery("SELECT id FROM proxies").WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(10))
	mock.ExpectQuery("SELECT EXISTS").WithArgs(sqlmock.AnyArg(), int64(3)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()
	if _, err := repo.Save(context.Background(), &service.ProxyTestTask{ID: 3, ProxyIDs: []int64{10}, Enabled: true}); err == nil {
		t.Fatal("must reject a proxy already belonging to another enabled task")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestProxyTaskManualRunDoesNotWriteAlertCycleOrSchedule(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	repo := NewProxyTestTaskRepository(db)
	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE proxy_test_tasks SET last_run_at=$2,last_run_status=$3,last_error=$4 WHERE id=$1`)).
		WithArgs(int64(3), nil, "manual", "").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta(`UPDATE proxy_test_task_targets SET last_node_status=$3,last_checked_at=$4,last_error=$5,last_result=$6 WHERE task_id=$1 AND proxy_id=$2`)).
		WithArgs(int64(3), int64(10), "healthy", nil, "", "null").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	err = repo.SaveRun(context.Background(), &service.ProxyTestTask{ID: 3, LastRunStatus: "manual", Targets: []*service.ProxyTestTaskTarget{{ProxyID: 10, LastNodeStatus: "healthy"}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
