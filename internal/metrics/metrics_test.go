package metrics_test

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/tulip/quicktun/internal/metrics"
)

// scrape exercises the registry through metrics.Handler and returns the
// response body — exactly what Prometheus does during a real scrape.
func scrape(t *testing.T, reg *prometheus.Registry) string {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/metrics", nil)
	metrics.Handler(reg).ServeHTTP(rec, req)
	require.Equal(t, 200, rec.Code, "scrape returned non-200")
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	require.NotEmpty(t, body, "empty scrape body")
	return string(body)
}

// TestRegistryExposesExpectedNames is a table-driven assertion that each
// binary's metric bundle, when registered + nudged, surfaces every expected
// metric name through the HTTP handler. Acts as both a sanity check and
// documentation of the metric contract.
func TestRegistryExposesExpectedNames(t *testing.T) {
	t.Run("server", func(t *testing.T) {
		reg := metrics.NewRegistry()
		m := metrics.NewServer(reg)
		m.ObserveRequest("/quicktun.v1.AuthService/Login", "OK", 0.01)
		m.IncSweeperFlipped(2)
		m.IncSupervisorRestarts("42")
		m.SetSupervisorAlive("42", true)

		body := scrape(t, reg)
		for _, name := range []string{
			"quicktun_server_requests_total",
			"quicktun_server_request_latency_seconds",
			"quicktun_server_sweeper_flipped_total",
			"quicktun_server_supervisor_restarts_total",
			"quicktun_server_supervisor_alive",
		} {
			require.True(t, strings.Contains(body, name), "missing metric %q", name)
		}
	})

	t.Run("authproxy", func(t *testing.T) {
		reg := metrics.NewRegistry()
		m := metrics.NewAuthProxy(reg)
		m.ObserveConnect("200", 0.005)
		m.ObserveConnect("401", 0.001)

		body := scrape(t, reg)
		for _, name := range []string{
			"quicktun_authproxy_connect_total",
			"quicktun_authproxy_connect_latency_seconds",
		} {
			require.True(t, strings.Contains(body, name), "missing metric %q", name)
		}
		require.Contains(t, body, `status="200"`)
		require.Contains(t, body, `status="401"`)
	})

	t.Run("agent", func(t *testing.T) {
		reg := metrics.NewRegistry()
		m := metrics.NewAgent(reg)
		m.ObserveBootstrap("ok")
		m.ObserveHeartbeat("err")
		m.SetSupervisorAlive(true)

		body := scrape(t, reg)
		for _, name := range []string{
			"quicktun_agent_bootstrap_total",
			"quicktun_agent_heartbeat_total",
			"quicktun_agent_supervisor_alive",
		} {
			require.True(t, strings.Contains(body, name), "missing metric %q", name)
		}
	})
}

// TestNilReceiverHelpersAreNoOps pins down the contract that every typed
// helper tolerates a nil receiver, so callers can wire instrumentation in
// without an `if m != nil` at every call site.
func TestNilReceiverHelpersAreNoOps(t *testing.T) {
	var sm *metrics.ServerMetrics
	sm.ObserveRequest("/x", "OK", 0)
	sm.IncSweeperFlipped(5)
	sm.IncSupervisorRestarts("1")
	sm.SetSupervisorAlive("1", true)
	sm.DeleteSupervisor("1")

	var ap *metrics.AuthProxyMetrics
	ap.ObserveConnect("200", 0)

	var ag *metrics.AgentMetrics
	ag.ObserveBootstrap("ok")
	ag.ObserveHeartbeat("ok")
	ag.SetSupervisorAlive(true)
}

// TestSweeperFlippedIgnoresNonPositive — guards against a bug where Tick
// passes 0 (no flips) and we'd accidentally render a Counter sample.
func TestSweeperFlippedIgnoresNonPositive(t *testing.T) {
	reg := metrics.NewRegistry()
	m := metrics.NewServer(reg)
	m.IncSweeperFlipped(0)
	m.IncSweeperFlipped(-3)
	body := scrape(t, reg)
	// The HELP/TYPE lines for the counter are always present (registry knows
	// it). What we assert is that no sample appears with a non-zero value.
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "quicktun_server_sweeper_flipped_total ") {
			// Counter line should be exactly " 0" since we never observed.
			require.Equal(t, "quicktun_server_sweeper_flipped_total 0", strings.TrimSpace(line))
		}
	}
}
