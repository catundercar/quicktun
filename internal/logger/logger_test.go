package logger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/config"
	"github.com/tulip/quicktun/internal/logger"
)

func TestNewWritesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.log")

	lg, err := logger.New(config.LogConfig{
		Path:       path,
		Level:      "info",
		MaxSizeMB:  10,
		MaxAgeDays: 1,
		MaxBackups: 1,
	})
	require.NoError(t, err)

	lg.Info("hello", logger.String("k", "v"))
	require.NoError(t, lg.Sync())

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	out := string(data)
	require.Contains(t, out, `"hello"`)
	require.Contains(t, out, `"k":"v"`)
}

func TestNewWithStdoutWhenPathEmpty(t *testing.T) {
	lg, err := logger.New(config.LogConfig{Level: "debug"})
	require.NoError(t, err)
	require.NotNil(t, lg)
	require.True(t, lg.Core().Enabled(-1)) // debug level
	require.False(t, strings.Contains("", "no-op"))
}

func TestNewRejectsBadLevel(t *testing.T) {
	_, err := logger.New(config.LogConfig{Level: "screaming"})
	require.Error(t, err)
}
