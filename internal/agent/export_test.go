package agent

import "time"

// HealthCheck exposes the unexported healthCheck for unit tests in the
// agent_test package. Production callers route through the HTTP /healthz
// handler instead.
func (r *Runtime) HealthCheck() (bool, []string) { return r.healthCheck() }

// SetLastBootstrapAtForTest seeds lastBootstrapAt without going through
// the full bootstrap RPC dance. Tests use this to drive the staleness
// branch of healthCheck deterministically.
func (r *Runtime) SetLastBootstrapAtForTest(t time.Time) {
	r.supMu.Lock()
	defer r.supMu.Unlock()
	r.lastBootstrapAt = t
}

// SetHeartbeatIntervalForTest seeds heartbeatInterval without running
// Run(). Tests use it to control the staleness threshold.
func (r *Runtime) SetHeartbeatIntervalForTest(d time.Duration) {
	r.supMu.Lock()
	defer r.supMu.Unlock()
	r.heartbeatInterval = d
}

// NewRuntimeForHealthTest builds a minimal Runtime that owns just enough
// state for healthCheck to be exercised without dialing the control plane.
// Returns nil-but-valid for everything else; do not Run() it.
func NewRuntimeForHealthTest(cfg *Config) *Runtime {
	return &Runtime{cfg: cfg}
}
