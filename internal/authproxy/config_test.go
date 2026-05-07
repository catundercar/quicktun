package authproxy_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/authproxy"
)

func writeYAML(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))
	return path
}

func TestLoadConfigValid(t *testing.T) {
	cfg, err := authproxy.LoadConfig(writeYAML(t, `
listen_addr: 127.0.0.1:9000
database:
  dsn: /tmp/quicktun.db
log:
  level: debug
`))
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9000", cfg.ListenAddr)
	require.Equal(t, "/tmp/quicktun.db", cfg.Database.DSN)
	require.Equal(t, "debug", cfg.Log.Level)
}

func TestLoadConfigAppliesDefaults(t *testing.T) {
	cfg, err := authproxy.LoadConfig(writeYAML(t, `
database:
  dsn: /tmp/x.db
`))
	require.NoError(t, err)
	require.Equal(t, ":8443", cfg.ListenAddr)
	require.Equal(t, "127.0.0.1:8444", cfg.HealthListenAddr)
	require.Equal(t, "info", cfg.Log.Level)
}

func TestLoadConfigRequiresDSN(t *testing.T) {
	_, err := authproxy.LoadConfig(writeYAML(t, `listen_addr: ":8443"`))
	require.Error(t, err)
	require.Contains(t, err.Error(), "database.dsn")
}

func TestLoadConfigParseError(t *testing.T) {
	_, err := authproxy.LoadConfig(writeYAML(t, `:::not yaml:::`))
	require.Error(t, err)
}
