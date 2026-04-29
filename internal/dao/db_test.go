package dao_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
)

func TestOpenInMemory(t *testing.T) {
	db, err := dao.Open("file::memory:?cache=shared")
	require.NoError(t, err)
	require.NotNil(t, db)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Ping())
}

func TestOpenFileWithWAL(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "test.db")
	dsn := "file:" + tmp + "?_journal_mode=WAL&_busy_timeout=5000"

	db, err := dao.Open(dsn)
	require.NoError(t, err)

	var mode string
	require.NoError(t, db.Raw("PRAGMA journal_mode").Scan(&mode).Error)
	require.Equal(t, "wal", mode)
}
