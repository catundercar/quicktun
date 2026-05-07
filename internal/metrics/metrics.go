// Package metrics provides a shared Prometheus registry and helpers for
// quicktun's three binaries. Each binary registers its own collectors via
// the package-level constructors (NewServer / NewAuthProxy / NewAgent) into
// the registry returned by NewRegistry; the binary then exposes the registry
// over HTTP via Handler.
//
// All collectors are nil-safe at the call site: callers may pass *ServerMetrics
// (etc.) as nil when metrics are disabled, and helper methods on the typed
// collectors short-circuit when the underlying receiver is nil. This keeps
// instrumentation hooks unobtrusive: callers don't need an `if m != nil` at
// every counter increment.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// NewRegistry returns a fresh prometheus.Registry pre-populated with the
// standard collectors (process + Go runtime). Each binary builds its own
// registry on startup so collector lifetimes are tied to the process and
// tests can spin up isolated registries without state leaking across them.
func NewRegistry() *prometheus.Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	r.MustRegister(collectors.NewGoCollector())
	return r
}

// Handler returns an http.Handler that serves the registry in Prometheus
// text format. The returned handler scopes its responses to r — callers may
// mount multiple handlers from independent registries on the same mux.
func Handler(r *prometheus.Registry) http.Handler {
	return promhttp.HandlerFor(r, promhttp.HandlerOpts{Registry: r})
}
