package agent_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/agent"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "agent.yaml")
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

func TestLoadValidConfig(t *testing.T) {
	p := writeConfig(t, `
control_endpoint: control.example.com:9090
token: raw-secret-token
state_dir: /tmp/qt-agent
rathole_binary: /usr/local/bin/rathole
rathole_args:
  - --client
  - --verbose
tls_insecure: true
hostname_override: bastion-1
`)
	cfg, err := agent.Load(p)
	require.NoError(t, err)
	require.Equal(t, "control.example.com:9090", cfg.ControlEndpoint)
	require.Equal(t, "raw-secret-token", cfg.Token)
	require.Equal(t, "/tmp/qt-agent", cfg.StateDir)
	require.Equal(t, "/usr/local/bin/rathole", cfg.RatholeBinary)
	require.Equal(t, []string{"--client", "--verbose"}, cfg.RatholeArgs)
	require.True(t, cfg.TLSInsecure)
	require.Equal(t, "bastion-1", cfg.HostnameOverride)
}

func TestLoadAppliesDefaults(t *testing.T) {
	p := writeConfig(t, `
control_endpoint: control.example.com:9090
token: t
`)
	cfg, err := agent.Load(p)
	require.NoError(t, err)
	require.Equal(t, "/var/lib/quicktun-agent", cfg.StateDir)
	require.Equal(t, []string{"--client"}, cfg.RatholeArgs)
	// RatholeBinary stays empty (render-only mode), per documented contract.
	require.Equal(t, "", cfg.RatholeBinary)
	require.False(t, cfg.TLSInsecure)
	// HealthListenAddr is off-by-default: operators must opt in.
	require.Equal(t, "", cfg.HealthListenAddr)
}

func TestLoadRequiresControlEndpoint(t *testing.T) {
	p := writeConfig(t, `
token: t
`)
	_, err := agent.Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "control_endpoint")
}

func TestLoadRequiresToken(t *testing.T) {
	p := writeConfig(t, `
control_endpoint: control.example.com:9090
`)
	_, err := agent.Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "token")
}

func TestLoadParseError(t *testing.T) {
	p := writeConfig(t, "this: is: not: valid: yaml: ::: :")
	_, err := agent.Load(p)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse config")
}
