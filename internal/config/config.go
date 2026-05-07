// Package config loads quicktun-server configuration from YAML.
//
// Config schema is defined by the Config struct; example:
//
//	control_plane:
//	  grpc_listen: 0.0.0.0:9443
//	  http_listen: 0.0.0.0:9080
//	database:
//	  driver: sqlite
//	  dsn: /opt/quicktun/var/quicktun.db?_journal_mode=WAL
//	session:
//	  default_ttl: 8h
//	log:
//	  path: /opt/quicktun/var/log/server.log
//	  level: info
package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// Config is the full server configuration tree.
type Config struct {
	ControlPlane ControlPlaneConfig `mapstructure:"control_plane"`
	Database     DatabaseConfig     `mapstructure:"database"`
	Session      SessionConfig      `mapstructure:"session"`
	Log          LogConfig          `mapstructure:"log"`
	Backend      BackendConfig      `mapstructure:"backend"`
}

// BackendConfig configures the relay backend (Phase 1: rathole) plus the
// observability surface (metrics endpoint, webhook alerts).
type BackendConfig struct {
	RatholeBinary       string        `mapstructure:"rathole_binary"`
	RatholeArgs         []string      `mapstructure:"rathole_args"`
	RatholeConfigDir    string        `mapstructure:"rathole_config_dir"`
	AuthProxyPublicAddr string        `mapstructure:"auth_proxy_public_addr"` // empty → legacy direct-rathole fallback
	SweeperInterval     time.Duration `mapstructure:"sweeper_interval"`       // 0 disables the sweeper
	SiteOfflineAfter    time.Duration `mapstructure:"site_offline_after"`     // 0 disables the sweeper

	// MetricsListenAddr, when set, opens a dedicated /metrics listener
	// in addition to the gateway-mounted endpoint. Loopback default
	// keeps Prometheus scrape traffic off the public interface.
	MetricsListenAddr string `mapstructure:"metrics_listen_addr"`

	// WebhookURL receives JSON event POSTs (currently: supervisor crash
	// loops). Empty disables alerting.
	WebhookURL         string        `mapstructure:"webhook_url"`
	WebhookTimeout     time.Duration `mapstructure:"webhook_timeout"`
	CrashLoopThreshold int           `mapstructure:"crash_loop_threshold"`
	CrashLoopWindow    time.Duration `mapstructure:"crash_loop_window"`
}

// ControlPlaneConfig holds gRPC + grpc-gateway listener settings.
type ControlPlaneConfig struct {
	GRPCListen string `mapstructure:"grpc_listen"`
	HTTPListen string `mapstructure:"http_listen"`
	RelayAddr  string `mapstructure:"relay_addr"`
}

// DatabaseConfig describes the persistence layer.
// Phase 1 only ships sqlite; Postgres support is Phase 3+.
type DatabaseConfig struct {
	Driver string `mapstructure:"driver"`
	DSN    string `mapstructure:"dsn"`
}

// SessionConfig controls operator login token lifetime.
type SessionConfig struct {
	DefaultTTL time.Duration `mapstructure:"default_ttl"`
}

// LogConfig sets up zap output destination and rotation policy.
type LogConfig struct {
	Path       string `mapstructure:"path"`
	Level      string `mapstructure:"level"`
	MaxSizeMB  int    `mapstructure:"max_size_mb"`
	MaxAgeDays int    `mapstructure:"max_age_days"`
	MaxBackups int    `mapstructure:"max_backups"`
}

// Load reads YAML from path and applies defaults for missing keys.
// Returns an error if the file cannot be read or parsed.
func Load(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("config: read %q: %w", path, err)
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("config: unmarshal: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Validate returns an error if any required field is missing or malformed.
// Called immediately after Load to fail fast at process startup rather than
// surfacing config bugs at first use.
func (c *Config) Validate() error {
	if c.Database.Driver == "" {
		return fmt.Errorf("config: database.driver is required")
	}
	if c.Database.Driver != "sqlite" {
		return fmt.Errorf("config: only sqlite driver supported in Phase 1, got %q", c.Database.Driver)
	}
	if c.Database.DSN == "" {
		return fmt.Errorf("config: database.dsn is required")
	}
	if c.ControlPlane.GRPCListen == "" {
		return fmt.Errorf("config: control_plane.grpc_listen is required")
	}
	if c.Session.DefaultTTL <= 0 {
		return fmt.Errorf("config: session.default_ttl must be positive")
	}
	return nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("control_plane.grpc_listen", "0.0.0.0:9443")
	v.SetDefault("control_plane.http_listen", "0.0.0.0:9080")
	v.SetDefault("control_plane.relay_addr", "relay.example.com:443")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("session.default_ttl", "8h")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.max_backups", 7)
	v.SetDefault("backend.rathole_binary", "rathole")
	v.SetDefault("backend.rathole_args", []string{"--server"})
	v.SetDefault("backend.rathole_config_dir", "/var/lib/quicktun/relays")
	v.SetDefault("backend.sweeper_interval", "30s")
	v.SetDefault("backend.site_offline_after", "90s")
	v.SetDefault("backend.metrics_listen_addr", "")
	v.SetDefault("backend.webhook_url", "")
	v.SetDefault("backend.webhook_timeout", "5s")
	v.SetDefault("backend.crash_loop_threshold", 5)
	v.SetDefault("backend.crash_loop_window", "5m")
}
