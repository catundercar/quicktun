package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/migration"
)

func TestAdminListOperators(t *testing.T) {
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

	// On a fresh DB, list-operators must succeed AND emit nothing.
	{
		root := &cobra.Command{Use: "root"}
		root.PersistentFlags().String("config", cfgPath, "")
		root.AddCommand(adminCmd())
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs([]string{"admin", "list-operators"})
		require.NoError(t, root.Execute())
		require.Empty(t, strings.TrimSpace(out.String()),
			"empty DB must list zero operators (got %q)", out.String())
	}

	// Create two operators (one admin, one not).
	for _, args := range [][]string{
		{"admin", "create-operator", "--email=alice@x.com", "--password=pw1", "--admin"},
		{"admin", "create-operator", "--email=bob@x.com", "--password=pw2"},
	} {
		root := &cobra.Command{Use: "root"}
		root.PersistentFlags().String("config", cfgPath, "")
		root.AddCommand(adminCmd())
		root.SetArgs(args)
		require.NoError(t, root.Execute())
	}

	// list-operators now emits two tab-separated rows.
	root := &cobra.Command{Use: "root"}
	root.PersistentFlags().String("config", cfgPath, "")
	root.AddCommand(adminCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"admin", "list-operators"})
	require.NoError(t, root.Execute())

	got := out.String()
	require.Contains(t, got, "alice@x.com")
	require.Contains(t, got, "bob@x.com")
	// Each line: "<id>\t<email>\t<is_admin>"
	lines := strings.Split(strings.TrimSpace(got), "\n")
	require.Len(t, lines, 2)
	for _, ln := range lines {
		parts := strings.Split(ln, "\t")
		require.Len(t, parts, 3, "line %q must have 3 tab-separated fields", ln)
		require.NotEmpty(t, parts[0], "id must be non-empty")
	}
	// is_admin column must reflect the create-operator flags.
	require.True(t, strings.Contains(got, "alice@x.com\ttrue"))
	require.True(t, strings.Contains(got, "bob@x.com\tfalse"))
}
