// Package agent implements the long-running site agent process that runs on
// a customer bastion host. It bootstraps against the quicktun control plane
// via gRPC, renders a rathole-client TOML config from the BootstrapResponse,
// and supervises the rathole client subprocess. A heartbeat loop refreshes
// last_seen_at and triggers a re-bootstrap whenever the server's
// ConfigVersion drifts from what the agent last applied.
package agent

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the agent's runtime config. Operators paste this into a YAML
// file (typically /etc/quicktun-agent.yaml) created by the install command.
type Config struct {
	// ControlEndpoint is the gRPC address of the quicktun control plane,
	// e.g., "control.example.com:9090". Required.
	ControlEndpoint string `yaml:"control_endpoint"`

	// Token is the raw agent token (operator copies from the install
	// command). Required. The agent presents this verbatim as a Bearer
	// credential and also computes sha256_hex(token) when rendering the
	// rathole client config (see docs/07-token-contract.md).
	Token string `yaml:"token"`

	// StateDir is where rathole-client.toml is rendered. Defaults to
	// /var/lib/quicktun-agent. Created on startup if missing.
	StateDir string `yaml:"state_dir"`

	// RatholeBinary is the path (or PATH-resolvable name) of the rathole
	// binary. Empty string activates render-only mode: the toml is written
	// but no subprocess is spawned. Useful for smoke tests and for
	// integration tests that just want to assert what the agent rendered.
	// Operators who want PATH lookup of "rathole" must set the field
	// explicitly — yaml.v3 cannot distinguish an omitted key from an
	// explicit empty string, so we treat empty as "render-only".
	RatholeBinary string `yaml:"rathole_binary"`

	// RatholeArgs are extra args appended after the rendered config path
	// when invoking RatholeBinary. Defaults to ["--client"] (rathole's
	// client mode flag).
	RatholeArgs []string `yaml:"rathole_args"`

	// TLSInsecure disables TLS verification on the gRPC dial AND lets the
	// bearer-token PerRPCCredentials work over a non-TLS transport. For
	// dev only; production deployments must set this false.
	TLSInsecure bool `yaml:"tls_insecure"`

	// HostnameOverride forces a hostname reported in Bootstrap/Heartbeat.
	// When empty, os.Hostname() is used.
	HostnameOverride string `yaml:"hostname_override"`

	// HealthListenAddr is the bind address for the agent's /healthz HTTP
	// endpoint. Empty (the default) disables it. Operators who want
	// systemd/launchd to probe should set e.g. "127.0.0.1:8445". Phase 1
	// keeps this off-by-default to avoid binding a port on hosts where the
	// operator isn't using probes.
	HealthListenAddr string `yaml:"health_listen_addr"`
}

// Load reads + parses a YAML config file, applies defaults, and validates
// required fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("agent: read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("agent: parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.StateDir == "" {
		c.StateDir = "/var/lib/quicktun-agent"
	}
	if len(c.RatholeArgs) == 0 {
		c.RatholeArgs = []string{"--client"}
	}
}

func (c *Config) validate() error {
	if c.ControlEndpoint == "" {
		return fmt.Errorf("agent: control_endpoint is required")
	}
	if c.Token == "" {
		return fmt.Errorf("agent: token is required")
	}
	return nil
}
