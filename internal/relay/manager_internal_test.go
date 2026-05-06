package relay

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRatholeSpawnArgsFlagsBeforeConfig pins down the argv ordering rule:
// rathole's CLI requires flags BEFORE the positional config path. The
// previous implementation placed cfgPath first, which produced
// `rathole <cfg> --server` — accepted only by our test fakebin (which
// ignores arg order) and silently broken against the real rathole.
func TestRatholeSpawnArgsFlagsBeforeConfig(t *testing.T) {
	got := ratholeSpawnArgs([]string{"--server"}, "/tmp/x.toml")
	require.Equal(t, []string{"--server", "/tmp/x.toml"}, got)
}

func TestRatholeSpawnArgsEmptyBinaryArgs(t *testing.T) {
	got := ratholeSpawnArgs(nil, "/tmp/x.toml")
	require.Equal(t, []string{"/tmp/x.toml"}, got)
}

// TestRatholeSpawnArgsDoesNotAliasInput guarantees the helper copies the
// caller's slice. If it didn't, mutating the returned slice would corrupt
// ManagerConfig.BinaryArgs across supervisors.
func TestRatholeSpawnArgsDoesNotAliasInput(t *testing.T) {
	in := []string{"--server"}
	got := ratholeSpawnArgs(in, "/tmp/x.toml")
	got[0] = "MUTATED"
	require.Equal(t, []string{"--server"}, in)
}
