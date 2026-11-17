//go:build unit

package repository

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestProxyMonitoringPostgresMetadataRoundtrip(t *testing.T) {
	db := scopedAdminKeyTestDB(t)
	_, err := db.Exec(`CREATE TABLE proxies (
 id BIGSERIAL PRIMARY KEY, name VARCHAR(100) NOT NULL, protocol VARCHAR(20) NOT NULL,
 host VARCHAR(255) NOT NULL, port INTEGER NOT NULL, username VARCHAR(100), password VARCHAR(100),
 status VARCHAR(20) NOT NULL DEFAULT 'active', expires_at TIMESTAMPTZ,
 fallback_mode VARCHAR(20) NOT NULL DEFAULT 'none', backup_proxy_id BIGINT, expiry_warn_days INTEGER DEFAULT 7,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	migration, err := os.ReadFile(filepath.Join("..", "..", "migrations", "251_proxy_test_tasks.sql"))
	require.NoError(t, err)
	_, err = db.Exec(string(migration))
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	repo := newProxyRepositoryWithSQL(client, db)
	ctx := context.Background()
	port := 17903
	p := &service.Proxy{Name: "Original US 1", Protocol: "socks5", Host: "127.0.0.1", Port: 7903,
		Status: service.StatusActive, FallbackMode: service.FallbackModeNone, ExpiryWarnDays: 7,
		NodeTestPort: &port, MihomoPrimaryNode: "us1", MihomoGroup: "us1-fallback"}
	require.NoError(t, repo.Create(ctx, p))
	loaded, err := repo.GetByID(ctx, p.ID)
	require.NoError(t, err)
	require.Equal(t, &port, loaded.NodeTestPort)
	require.Equal(t, "us1", loaded.MihomoPrimaryNode)
	require.Equal(t, "us1-fallback", loaded.MihomoGroup)
	require.Equal(t, service.ProxyProbeConfigVersion(p), service.ProxyProbeConfigVersion(loaded),
		"a freshly persisted Ent entity and a DB read must agree on the cache fence")

	loaded.NodeTestPort, loaded.MihomoPrimaryNode, loaded.MihomoGroup = nil, "", ""
	require.NoError(t, repo.Update(ctx, loaded))
	cleared, err := repo.GetByID(ctx, p.ID)
	require.NoError(t, err)
	require.Nil(t, cleared.NodeTestPort)
	require.Empty(t, cleared.MihomoPrimaryNode)
	require.Empty(t, cleared.MihomoGroup)
	require.Equal(t, service.ProxyProbeConfigVersion(loaded), service.ProxyProbeConfigVersion(cleared))

	// Exercise values on both sides of PostgreSQL's microsecond tie rounding.
	for _, fraction := range []string{"123456123", "123456500", "123456789", "123457500", "999999789"} {
		builderTime, err := time.Parse(time.RFC3339Nano, "2026-11-17T12:00:00."+fraction+"+08:00")
		require.NoError(t, err)
		var storedTime time.Time
		require.NoError(t, db.QueryRow(`SELECT $1::timestamptz`, builderTime).Scan(&storedTime))
		builder, fromDB := *p, *p
		builder.UpdatedAt, fromDB.UpdatedAt = builderTime, storedTime
		require.Equal(t, service.ProxyProbeConfigVersion(&builder), service.ProxyProbeConfigVersion(&fromDB), fraction)
	}
}
