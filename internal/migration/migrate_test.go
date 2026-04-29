package migration_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/migration"
)

func TestUpAppliesAllTables(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "qt.db")
	dsn := "file:" + tmp + "?_foreign_keys=on"

	require.NoError(t, migration.Up(dsn))

	db, err := dao.Open(dsn)
	require.NoError(t, err)

	wanted := []string{
		"operators", "operator_sessions",
		"projects", "operator_project_access",
		"sites", "services", "site_agent_tokens",
		"audit_logs",
	}
	for _, table := range wanted {
		var count int
		require.NoError(t, db.Raw(
			"SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&count).Error)
		require.Equal(t, 1, count, "expected table %q to exist", table)
	}
}

func TestUpIsIdempotent(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "qt.db")
	dsn := "file:" + tmp

	require.NoError(t, migration.Up(dsn))
	require.NoError(t, migration.Up(dsn)) // second call should be no-op
}

func TestDownDropsAllTables(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "qt.db")
	dsn := "file:" + tmp

	require.NoError(t, migration.Up(dsn))
	require.NoError(t, migration.Down(dsn))

	db, err := dao.Open(dsn)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='operators'",
	).Scan(&count).Error)
	require.Equal(t, 0, count)
}
