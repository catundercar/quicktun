package metrics

import "github.com/prometheus/client_golang/prometheus"

// AgentMetrics holds collectors for the site-agent daemon.
//
// The agent supervises exactly one rathole-client, so SupervisorAlive is a
// simple Gauge (no labels) rather than a GaugeVec.
type AgentMetrics struct {
	BootstrapTotal  *prometheus.CounterVec // result ("ok"|"err")
	HeartbeatTotal  *prometheus.CounterVec // result ("ok"|"err")
	SupervisorAlive prometheus.Gauge
}

// NewAgent registers the agent's collectors against r. Panics if r is nil.
func NewAgent(r *prometheus.Registry) *AgentMetrics {
	m := &AgentMetrics{
		BootstrapTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quicktun_agent_bootstrap_total",
			Help: "Bootstrap RPCs attempted by the agent, labelled by outcome.",
		}, []string{"result"}),
		HeartbeatTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "quicktun_agent_heartbeat_total",
			Help: "Heartbeat RPCs attempted by the agent, labelled by outcome.",
		}, []string{"result"}),
		SupervisorAlive: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "quicktun_agent_supervisor_alive",
			Help: "1 when the rathole-client subprocess is currently running, 0 when stopped.",
		}),
	}
	r.MustRegister(m.BootstrapTotal, m.HeartbeatTotal, m.SupervisorAlive)
	return m
}

// ObserveBootstrap records one bootstrap outcome ("ok"|"err"). No-op when m is nil.
func (m *AgentMetrics) ObserveBootstrap(result string) {
	if m == nil {
		return
	}
	m.BootstrapTotal.WithLabelValues(result).Inc()
}

// ObserveHeartbeat records one heartbeat outcome. No-op when m is nil.
func (m *AgentMetrics) ObserveHeartbeat(result string) {
	if m == nil {
		return
	}
	m.HeartbeatTotal.WithLabelValues(result).Inc()
}

// SetSupervisorAlive flips the gauge. No-op when m is nil.
func (m *AgentMetrics) SetSupervisorAlive(alive bool) {
	if m == nil {
		return
	}
	v := 0.0
	if alive {
		v = 1.0
	}
	m.SupervisorAlive.Set(v)
}
