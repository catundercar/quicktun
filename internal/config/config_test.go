package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/config"
)

func TestLoadFromYAML(t *testing.T) {
	yaml := `
control_plane:
  grpc_listen: 0.0.0.0:9443
  http_listen: 0.0.0.0:9080

database:
  driver: sqlite
  dsn: /tmp/qt.db?_journal_mode=WAL

session:
  default_ttl: 8h

log:
  path: /tmp/qt.log
  level: info
  max_size_mb: 100
  max_age_days: 30
  max_backups: 7
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9443", cfg.ControlPlane.GRPCListen)
	require.Equal(t, "sqlite", cfg.Database.Driver)
	require.Equal(t, "/tmp/qt.db?_journal_mode=WAL", cfg.Database.DSN)
	require.Equal(t, 8*time.Hour, cfg.Session.DefaultTTL)
	require.Equal(t, "info", cfg.Log.Level)
}

func TestLoadAppliesDefaults(t *testing.T) {
	// Minimal config — only DSN required.
	yaml := `
database:
  dsn: ":memory:"
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, "0.0.0.0:9443", cfg.ControlPlane.GRPCListen)
	require.Equal(t, "info", cfg.Log.Level)
	require.Equal(t, 8*time.Hour, cfg.Session.DefaultTTL)
}

func TestLoadFailsOnMissingFile(t *testing.T) {
	_, err := config.Load("/no/such/file.yaml")
	require.Error(t, err)
}

func TestValidateRejectsEmptyDSN(t *testing.T) {
	yaml := `
database:
  driver: sqlite
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	_, err := config.Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "database.dsn is required")
}

func TestValidateRejectsBadDriver(t *testing.T) {
	yaml := `
database:
  driver: postgres
  dsn: "host=localhost"
`
	path := filepath.Join(t.TempDir(), "server.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))

	_, err := config.Load(path)
	require.Error(t, err)
	require.Contains(t, err.Error(), "only sqlite driver supported")
}
