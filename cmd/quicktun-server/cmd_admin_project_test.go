package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/migration"
)

func TestAdminProjectCreate(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "qt.db")
	cfgPath := filepath.Join(dir, "server.yaml")
	yaml := `
control_plane:
  grpc_listen: 127.0.0.1:9443
database:
  driver: sqlite
  dsn: ` + dbPath + `?_foreign_keys=on
session:
  default_ttl: 1h
log:
  level: error
`
	require.NoError(t, os.WriteFile(cfgPath, []byte(yaml), 0o600))
	require.NoError(t, migration.Up(dbPath+"?_foreign_keys=on"))

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", cfgPath, "")
	root.AddCommand(adminCmd())
	root.SetArgs([]string{"admin", "project", "create",
		"--slug=clinic-network",
		"--display-name=Clinic Network",
		"--relay-port-range=20000-20999",
	})
	require.NoError(t, root.Execute())

	db, err := dao.Open(dbPath, nil)
	require.NoError(t, err)
	p, err := dao.NewProjectDAO(db).FindBySlug(context.Background(), "clinic-network")
	require.NoError(t, err)
	require.Equal(t, "Clinic Network", p.Name)
	require.Equal(t, "20000-20999", p.RelayPortRange)
}
