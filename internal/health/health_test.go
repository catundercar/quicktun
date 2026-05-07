package health_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/health"
)

func TestHandlerOk(t *testing.T) {
	h := health.Handler(func() (bool, []string) { return true, nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "ok", body["status"])
	_, hasReasons := body["reasons"]
	require.False(t, hasReasons, "ok response should not include reasons field")
}

func TestHandlerDegraded(t *testing.T) {
	h := health.Handler(func() (bool, []string) {
		return false, []string{"db down", "supervisor not running"}
	})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, "degraded", body["status"])
	reasons, ok := body["reasons"].([]any)
	require.True(t, ok, "reasons should be an array")
	require.Equal(t, []any{"db down", "supervisor not running"}, reasons)
}
