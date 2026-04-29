package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
)

func TestMigrateCmdAppliesSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qt.db")
	cfgPath := filepath.Join(dir, "server.yaml")

	yaml := `
database:
  driver: sqlite
  dsn: ` + dbPath + `?_foreign_keys=on
log:
  level: error
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o600))

	// Wire migrate under a fake root so it inherits the persistent --config flag
	// the same way it does in production.
	root := &cobra.Command{Use: "test-root"}
	root.PersistentFlags().String("config", cfgPath, "path to YAML config")
	cmd := migrateCmd()
	root.AddCommand(cmd)

	require.NoError(t, cmd.RunE(cmd, nil))

	db, err := dao.Open(dbPath)
	require.NoError(t, err)
	var n int
	require.NoError(t, db.Raw(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='operators'",
	).Scan(&n).Error)
	require.Equal(t, 1, n)
}
