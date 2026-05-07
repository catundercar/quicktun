// Package health provides a tiny http.Handler for /healthz endpoints.
// All three quicktun binaries (server, authproxy, agent) mount this on a
// dedicated HTTP path so systemd / launchd / nginx / k8s probes can verify
// liveness without speaking gRPC.
package health

import (
	"encoding/json"
	"net/http"
)

// Checker reports a service's health. Reasons is empty when ok=true; when
// ok=false, it should contain one or more short, human-readable strings
// describing what is wrong (e.g. "db ping: connection refused").
type Checker func() (ok bool, reasons []string)

// Handler returns an http.Handler that runs check on every request and
// renders {"status": "ok"} (200) or {"status": "degraded", "reasons": [...]}
// (503) JSON. Callers typically register it on a dedicated /healthz route.
func Handler(check Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		ok, reasons := check()
		body := map[string]any{"status": "ok"}
		statusCode := http.StatusOK
		if !ok {
			body["status"] = "degraded"
			body["reasons"] = reasons
			statusCode = http.StatusServiceUnavailable
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_ = json.NewEncoder(w).Encode(body)
	})
}
