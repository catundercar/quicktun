package metrics

import "github.com/prometheus/client_golang/prometheus"

// AuthProxyMetrics holds collectors for the auth-proxy daemon.
type AuthProxyMetrics struct {
	ConnectTotal          *prometheus.CounterVec   // status (e.g. "200", "401", "405", "500", "502")
	ConnectLatencySeconds *prometheus.HistogramVec // status
}

// NewAuthProxy registers the auth-proxy collectors against r and returns
// the bundle. Panics if r is nil.
func NewAuthProxy(r *prometheus.Registry) *AuthProxyMetrics {
	m := &AuthProxyMetrics{
		ConnectTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quicktun_authproxy_connect_total",
			Help: "CONNECT requests handled by auth-proxy, labelled by HTTP status reply.",
		}, []string{"status"}),
		ConnectLatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "quicktun_authproxy_connect_latency_seconds",
			Help: "Time from CONNECT receipt to status-line write, labelled by status.",
			// CONNECT setup is fast (~ms); finer buckets in the sub-second range.
			Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"status"}),
	}
	r.MustRegister(m.ConnectTotal, m.ConnectLatencySeconds)
	return m
}

// ObserveConnect records one CONNECT outcome. status is the numeric HTTP
// status as a string ("200", "401", ...). No-op when m is nil.
func (m *AuthProxyMetrics) ObserveConnect(status string, latencySeconds float64) {
	if m == nil {
		return
	}
	m.ConnectTotal.WithLabelValues(status).Inc()
	m.ConnectLatencySeconds.WithLabelValues(status).Observe(latencySeconds)
}
