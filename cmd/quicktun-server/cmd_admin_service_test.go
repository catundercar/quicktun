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
	"github.com/tulip/quicktun/internal/model"
)

func TestAdminServiceCreate(t *testing.T) {
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

	db, err := dao.Open(dbPath, nil)
	require.NoError(t, err)
	p, err := dao.NewProjectDAO(db).Create(context.Background(), &model.Project{
		Slug: "clinic-network", Name: "Clinic", RelayPortRange: "20000-20099",
	})
	require.NoError(t, err)
	_, err = dao.NewSiteDAO(db).Create(context.Background(), &model.Site{
		ProjectID: p.ID, Name: "bastion-1",
	})
	require.NoError(t, err)
	s, _ := db.DB()
	s.Close()

	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", cfgPath, "")
	root.AddCommand(adminCmd())
	root.SetArgs([]string{"admin", "service", "create",
		"--project=clinic-network",
		"--site=bastion-1",
		"--slug=ssh",
		"--target-addr=127.0.0.1",
		"--target-port=22",
	})
	require.NoError(t, root.Execute())
}
