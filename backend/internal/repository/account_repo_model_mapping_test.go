package repository

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

func TestBulkUpdateAppendModelMappingReportsPerAccountChanges(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*ORDER BY id FOR UPDATE`).
		WithArgs(`{1,2}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"a":"x"}`)).
			AddRow(int64(2), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"b":"y"}`)))
	mock.ExpectExec(`(?s)UPDATE accounts SET credentials = jsonb_set.*credentials IS DISTINCT FROM`).
		WithArgs([]byte(`{"a":"x"}`), `{2,1,2}`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, []byte(`{"account_ids":[2]}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	stats := make(map[int64]service.ModelMappingAppendStats)
	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{2, 1, 2}, service.AccountBulkUpdate{
		ModelMappingMode: "append",
		Credentials:      map[string]any{"model_mapping": map[string]any{"a": "x"}},
		MappingStats:     stats,
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	require.Equal(t, service.ModelMappingAppendStats{Unchanged: 1, NoChanges: true}, stats[1])
	require.Equal(t, service.ModelMappingAppendStats{Added: 1}, stats[2])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateAppendModelMappingNoOpSkipsOutbox(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"a":"x"}`)))
	mock.ExpectExec(`(?s)UPDATE accounts SET credentials = jsonb_set.*credentials IS DISTINCT FROM`).
		WithArgs([]byte(`{"a":"x"}`), `{1}`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	stats := make(map[int64]service.ModelMappingAppendStats)
	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{1}, service.AccountBulkUpdate{
		ModelMappingMode: "append",
		Credentials:      map[string]any{"model_mapping": map[string]any{"a": "x"}},
		MappingStats:     stats,
	})
	require.NoError(t, err)
	require.Zero(t, rows)
	require.Equal(t, service.ModelMappingAppendStats{Unchanged: 1, NoChanges: true}, stats[1])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateAppendModelMappingWithBaseURLStillUpdatesAllTargets(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1,2}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"a":"x"}`)).
			AddRow(int64(2), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"a":"x"}`)))
	mock.ExpectExec(`(?s)UPDATE accounts SET credentials = jsonb_set.*WHERE id = ANY\(\$3\) AND deleted_at IS NULL$`).
		WithArgs([]byte(`{"base_url":"https://example.com"}`), []byte(`{"a":"x"}`), `{2,1}`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, []byte(`{"account_ids":[2,1]}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	stats := make(map[int64]service.ModelMappingAppendStats)
	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{2, 1}, service.AccountBulkUpdate{
		ModelMappingMode: "append",
		Credentials: map[string]any{
			"base_url":      "https://example.com",
			"model_mapping": map[string]any{"a": "x"},
		},
		MappingStats: stats,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), rows)
	require.Equal(t, 1, stats[1].Unchanged)
	require.False(t, stats[1].NoChanges)
	require.False(t, stats[2].NoChanges)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateAppendModelMappingRejectsInvalidStoredMapping(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`["invalid"]`)))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.BulkUpdate(context.Background(), []int64{1}, service.AccountBulkUpdate{
		ModelMappingMode: "append",
		Credentials:      map[string]any{"model_mapping": map[string]any{"a": "x"}},
	})
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "INVALID_STORED_MODEL_MAPPING", appErr.Reason)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateAppendModelMappingRejectsDefaultMappingReplacement(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformGrok, service.AccountTypeAPIKey, nil, nil))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.BulkUpdate(context.Background(), []int64{1}, service.AccountBulkUpdate{
		ModelMappingMode: "append",
		Credentials:      map[string]any{"model_mapping": map[string]any{"a": "x"}},
	})
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "MODEL_MAPPING_APPEND_DEFAULT_CONFLICT", appErr.Reason)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateReplaceMappingsPreservesPerAccountWhitelistSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*ORDER BY id FOR UPDATE`).
		WithArgs(`{1,2}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"whitelist-one":"whitelist-one","old":"target"}`)).
			AddRow(int64(2), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"whitelist-two":"whitelist-two","old":"target"}`)))
	mock.ExpectExec(`(?s)UPDATE accounts SET credentials = jsonb_set.*jsonb_object_agg\(entry.key, entry.value\).*WHERE entry.value = to_jsonb\(entry.key\).*WHERE id = ANY\(\$2\) AND deleted_at IS NULL$`).
		WithArgs([]byte(`{"new":"target"}`), `{2,1}`).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, []byte(`{"account_ids":[2,1]}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{2, 1}, service.AccountBulkUpdate{
		ModelMappingMode: "replace_mappings",
		Credentials:      map[string]any{"model_mapping": map[string]any{"new": "target"}},
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateReplaceMappingsRejectsWhitelistConflictBeforeWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1,2}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"old":"target"}`)).
			AddRow(int64(2), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"new":"new"}`)))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.BulkUpdate(context.Background(), []int64{1, 2}, service.AccountBulkUpdate{
		ModelMappingMode: "replace_mappings",
		Credentials:      map[string]any{"model_mapping": map[string]any{"new": "target"}},
	})
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "MODEL_MAPPING_REPLACE_WHITELIST_CONFLICT", appErr.Reason)
	require.Equal(t, "2", appErr.Metadata["account_id"])
	require.Equal(t, "new", appErr.Metadata["key"])
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateReplaceMappingsEmptyClearsOnlyAliasesSQL(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"allowed":"allowed","old":"target"}`)))
	mock.ExpectExec(`(?s)UPDATE accounts SET credentials = jsonb_set.*jsonb_object_agg\(entry.key, entry.value\).*WHERE entry.value = to_jsonb\(entry.key\)`).
		WithArgs([]byte(`{}`), `{1}`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(regexp.QuoteMeta("INSERT INTO scheduler_outbox")).
		WithArgs(service.SchedulerOutboxEventAccountBulkChanged, nil, nil, []byte(`{"account_ids":[1]}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	rows, err := repo.BulkUpdate(context.Background(), []int64{1}, service.AccountBulkUpdate{
		ModelMappingMode: "replace_mappings",
		Credentials:      map[string]any{"model_mapping": map[string]any{}},
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), rows)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestBulkUpdateReplaceMappingsRejectsNoncanonicalWhitelist(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, platform, type, credentials ->> 'oauth_type', credentials -> 'model_mapping'.*FOR UPDATE`).
		WithArgs(`{1}`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "platform", "type", "oauth_type", "model_mapping"}).
			AddRow(int64(1), service.PlatformOpenAI, service.AccountTypeAPIKey, nil, []byte(`{"allowed ":"allowed"}`)))
	mock.ExpectRollback()

	repo := newAccountRepositoryWithSQL(client, db, nil)
	_, err = repo.BulkUpdate(context.Background(), []int64{1}, service.AccountBulkUpdate{
		ModelMappingMode: "replace_mappings",
		Credentials:      map[string]any{"model_mapping": map[string]any{}},
	})
	var appErr *infraerrors.ApplicationError
	require.ErrorAs(t, err, &appErr)
	require.Equal(t, "NONCANONICAL_MODEL_MAPPING_WHITELIST", appErr.Reason)
	require.Equal(t, "1", appErr.Metadata["account_id"])
	require.Equal(t, "allowed ", appErr.Metadata["key"])
	require.NoError(t, mock.ExpectationsWereMet())
}
