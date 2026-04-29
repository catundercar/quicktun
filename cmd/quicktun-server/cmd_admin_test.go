package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/tulip/quicktun/internal/dao"
	"github.com/tulip/quicktun/internal/migration"
)

func TestAdminCreateOperator(t *testing.T) {
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
	cmd := adminCmd()
	root.AddCommand(cmd)

	root.SetArgs([]string{"admin", "create-operator", "--email=admin@x.com", "--password=hunter2", "--admin"})
	require.NoError(t, root.Execute())

	db, err := dao.Open(dbPath, nil)
	require.NoError(t, err)
	op, err := dao.NewOperatorDAO(db).FindByEmail(context.Background(), "admin@x.com")
	require.NoError(t, err)
	require.True(t, op.IsAdmin)
	require.NoError(t, bcrypt.CompareHashAndPassword([]byte(op.PasswordHash), []byte("hunter2")))
}
