package installscripts_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/server/installscripts"
)

func TestHandlerServesAgentScript(t *testing.T) {
	srv := httptest.NewServer(installscripts.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/install/agent.sh")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	ct := resp.Header.Get("Content-Type")
	require.True(t, strings.HasPrefix(ct, "text/x-shellscript"),
		"unexpected Content-Type %q", ct)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	s := string(body)
	require.Contains(t, s, "#!/usr/bin/env bash")
	require.Contains(t, s, "QT_TOKEN")
	require.Contains(t, s, "QT_ENDPOINT")
	// Self-contained — must not source any sibling lib file.
	require.NotContains(t, s, "source ")
	require.NotContains(t, s, ". lib.sh")
}

func TestHandlerServesAgentPs1(t *testing.T) {
	srv := httptest.NewServer(installscripts.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/install/agent.ps1")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	s := string(body)
	require.Contains(t, s, "#Requires -RunAsAdministrator")
	require.Contains(t, s, "QT_TOKEN")
	require.Contains(t, s, "QT_ENDPOINT")
	require.Contains(t, s, "service-run")
	// Self-contained — must not source any sibling lib.
	require.NotContains(t, s, ". .\\lib.ps1")
}

func TestHandlerReturns404ForUnknownFile(t *testing.T) {
	srv := httptest.NewServer(installscripts.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/install/nonexistent.sh")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}
