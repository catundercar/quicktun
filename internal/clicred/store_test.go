package clicred

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// lstat is a tiny indirection so the test reads naturally even though
// it's just os.Lstat under the hood; keeps imports tidy in callers.
func lstat(p string) (os.FileInfo, error) { return os.Lstat(p) }

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "credentials.yaml")

	in := &Credentials{
		Endpoint:          "control.example.com:9090",
		AuthProxyEndpoint: "auth.example.com:8443",
		SessionToken:      "tok-abc",
		OperatorEmail:     "op@example.com",
		TLSInsecure:       true,
	}
	require.NoError(t, Save(path, in))

	got, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, in, got)
}

func TestDefaultPathHonorsQuicktunConfig(t *testing.T) {
	t.Setenv("QUICKTUN_CONFIG", "/foo/bar.yaml")
	t.Setenv("XDG_CONFIG_HOME", "/should/not/win")
	t.Setenv("HOME", "/should/not/win/either")

	got, err := DefaultPath()
	require.NoError(t, err)
	require.Equal(t, "/foo/bar.yaml", got)
}

func TestDefaultPathHonorsXDG(t *testing.T) {
	t.Setenv("QUICKTUN_CONFIG", "")
	t.Setenv("XDG_CONFIG_HOME", "/x")

	got, err := DefaultPath()
	require.NoError(t, err)
	require.True(t, strings.HasSuffix(got, "/x/quicktun/credentials.yaml"),
		"got %q, want suffix /x/quicktun/credentials.yaml", got)
}

func TestSavedFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "qt", "credentials.yaml")

	require.NoError(t, Save(path, &Credentials{Endpoint: "x:1"}))

	fi, err := lstat(path)
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o600), fi.Mode().Perm(), "file mode")

	di, err := lstat(filepath.Dir(path))
	require.NoError(t, err)
	require.Equal(t, fs.FileMode(0o700), di.Mode().Perm(), "parent dir mode")
}

func TestLoadMissingFileReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope.yaml")

	_, err := Load(path)
	require.Error(t, err)
	require.True(t, errors.Is(err, fs.ErrNotExist),
		"expected fs.ErrNotExist, got %T %v", err, err)
}
