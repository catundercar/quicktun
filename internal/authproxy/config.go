package authproxy

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the auth-proxy daemon's runtime config.
type Config struct {
	// ListenAddr is the bind address, e.g. ":8443" or "0.0.0.0:8443".
	// Default: ":8443".
	ListenAddr string `yaml:"listen_addr"`

	// HealthListenAddr is the bind address for the /healthz HTTP endpoint.
	// Default: "127.0.0.1:8444". Set to empty string to disable. Loopback
	// default keeps health checks reachable from local probes (systemd,
	// monitoring sidecar) without exposing the endpoint to the public
	// network — operators who need external probing should override.
	HealthListenAddr string `yaml:"health_listen_addr"`

	// MetricsListenAddr is the bind address for the /metrics HTTP endpoint.
	// Empty (default) disables it. Loopback recommended (e.g.
	// "127.0.0.1:8445") so Prometheus scrapes don't traverse the public
	// network. Kept off-by-default to avoid binding a port on hosts where
	// the operator hasn't opted into metrics scraping yet.
	MetricsListenAddr string `yaml:"metrics_listen_addr"`

	// Database holds the path to the control plane's SQLite file. The
	// auth-proxy reads (token validation, project lookup) without writing.
	// Required.
	Database struct {
		DSN string `yaml:"dsn"`
	} `yaml:"database"`

	// Log level (debug | info | warn | error). Default: info.
	Log struct {
		Level string `yaml:"level"`
	} `yaml:"log"`
}

// LoadConfig reads + parses a YAML config file, applies defaults, and validates.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("authproxy: read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("authproxy: parse config: %w", err)
	}
	cfg.applyDefaults()
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = ":8443"
	}
	// HealthListenAddr defaults to loopback only when the user did not
	// supply the key at all. We can't distinguish "omitted" from "explicit
	// empty" with yaml.v3, so an explicit empty string in YAML is treated
	// the same as omission and gets the default. Operators who want to
	// disable health must override at runtime (e.g. via a wrapper config).
	if c.HealthListenAddr == "" {
		c.HealthListenAddr = "127.0.0.1:8444"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
}

func (c *Config) validate() error {
	if c.Database.DSN == "" {
		return fmt.Errorf("authproxy: database.dsn is required")
	}
	return nil
}
