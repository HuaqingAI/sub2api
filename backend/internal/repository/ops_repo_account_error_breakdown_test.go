package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetAccountErrorBreakdownGroupsCategoriesByAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	mock.ExpectQuery(`(?s)SELECT\s+e\.account_id.*GROUP BY e\.account_id, a\.name, e\.error_phase, e\.error_type`).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "account_name", "error_phase", "error_type", "total"}).
			AddRow(int64(1), "primary", "upstream", "upstream_error", int64(3)).
			AddRow(int64(1), "primary", "request", "rate_limit_error", int64(5)).
			AddRow(int64(1), "primary", "internal", "internal_error", int64(1)).
			AddRow(int64(2), "secondary", "upstream", "upstream_error", int64(2)))

	repo := &opsRepository{db: db}
	result, err := repo.GetAccountErrorBreakdown(context.Background(), &service.OpsErrorLogFilter{View: "all"})
	require.NoError(t, err)
	require.NoError(t, mock.ExpectationsWereMet())
	require.Equal(t, []*service.OpsAccountErrorBreakdownItem{
		{
			AccountID:   1,
			AccountName: "primary",
			Total:       9,
			Categories: []*service.OpsAccountErrorCategoryCount{
				{Category: "internal", Count: 1},
				{Category: "rate_limit", Count: 5},
				{Category: "upstream", Count: 3},
			},
		},
		{
			AccountID:   2,
			AccountName: "secondary",
			Total:       2,
			Categories: []*service.OpsAccountErrorCategoryCount{
				{Category: "upstream", Count: 2},
			},
		},
	}, result.Accounts)
}
