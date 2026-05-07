package metrics

import "github.com/prometheus/client_golang/prometheus"

// ServerMetrics holds collectors for the control-plane (quicktun-server).
//
// All methods on this type are nil-safe — passing a nil *ServerMetrics to a
// helper (or holding a nil reference and calling its methods) is a no-op so
// callers can wire metrics in without sprinkling `if m != nil` everywhere.
type ServerMetrics struct {
	RequestsTotal           *prometheus.CounterVec   // method, code
	RequestLatencySeconds   *prometheus.HistogramVec // method
	SweeperFlippedTotal     prometheus.Counter
	SupervisorRestartsTotal *prometheus.CounterVec // project_id
	SupervisorAlive         *prometheus.GaugeVec   // project_id
}

// NewServer constructs the control-plane metrics and registers every
// collector with r. It panics if r is nil — that's a programming error.
func NewServer(r *prometheus.Registry) *ServerMetrics {
	m := &ServerMetrics{
		RequestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quicktun_server_requests_total",
			Help: "gRPC requests served by the control plane, labelled by method and status code.",
		}, []string{"method", "code"}),
		RequestLatencySeconds: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "quicktun_server_request_latency_seconds",
			Help:    "gRPC request latency in seconds, labelled by method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method"}),
		SweeperFlippedTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "quicktun_server_sweeper_flipped_total",
			Help: "Sites flipped from online to offline by the liveness sweeper.",
		}),
		SupervisorRestartsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quicktun_server_supervisor_restarts_total",
			Help: "Number of times a supervised rathole-server child has exited (and been restarted), labelled by project_id.",
		}, []string{"project_id"}),
		SupervisorAlive: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "quicktun_server_supervisor_alive",
			Help: "1 when the supervised rathole-server for a project is currently running, 0 when stopped.",
		}, []string{"project_id"}),
	}
	r.MustRegister(
		m.RequestsTotal,
		m.RequestLatencySeconds,
		m.SweeperFlippedTotal,
		m.SupervisorRestartsTotal,
		m.SupervisorAlive,
	)
	return m
}

// ObserveRequest records one gRPC request outcome. Safe to call on a nil
// *ServerMetrics — it short-circuits. method is the gRPC FullMethod
// ("/quicktun.v1.AuthService/Login"); code is the canonical status code
// name ("OK", "Unauthenticated", ...).
func (m *ServerMetrics) ObserveRequest(method, code string, latencySeconds float64) {
	if m == nil {
		return
	}
	m.RequestsTotal.WithLabelValues(method, code).Inc()
	m.RequestLatencySeconds.WithLabelValues(method).Observe(latencySeconds)
}

// IncSweeperFlipped bumps the sweeper counter by n. No-op when m is nil
// or n <= 0.
func (m *ServerMetrics) IncSweeperFlipped(n int64) {
	if m == nil || n <= 0 {
		return
	}
	m.SweeperFlippedTotal.Add(float64(n))
}

// IncSupervisorRestarts increments the per-project supervisor restart
// counter. No-op when m is nil.
func (m *ServerMetrics) IncSupervisorRestarts(projectID string) {
	if m == nil {
		return
	}
	m.SupervisorRestartsTotal.WithLabelValues(projectID).Inc()
}

// SetSupervisorAlive sets the per-project alive gauge. No-op when m is nil.
func (m *ServerMetrics) SetSupervisorAlive(projectID string, alive bool) {
	if m == nil {
		return
	}
	v := 0.0
	if alive {
		v = 1.0
	}
	m.SupervisorAlive.WithLabelValues(projectID).Set(v)
}

// DeleteSupervisor removes the per-project gauge + restart counter label
// series. Call from RemoveProject so retired projects don't leak label
// cardinality. No-op when m is nil.
func (m *ServerMetrics) DeleteSupervisor(projectID string) {
	if m == nil {
		return
	}
	m.SupervisorAlive.DeleteLabelValues(projectID)
	m.SupervisorRestartsTotal.DeleteLabelValues(projectID)
}
