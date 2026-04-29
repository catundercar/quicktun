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
}

// ControlPlaneConfig holds gRPC + grpc-gateway listener settings.
type ControlPlaneConfig struct {
	GRPCListen string `mapstructure:"grpc_listen"`
	HTTPListen string `mapstructure:"http_listen"`
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
	return &cfg, nil
}

func setDefaults(v *viper.Viper) {
	v.SetDefault("control_plane.grpc_listen", "0.0.0.0:9443")
	v.SetDefault("control_plane.http_listen", "0.0.0.0:9080")
	v.SetDefault("database.driver", "sqlite")
	v.SetDefault("session.default_ttl", "8h")
	v.SetDefault("log.level", "info")
	v.SetDefault("log.max_size_mb", 100)
	v.SetDefault("log.max_age_days", 30)
	v.SetDefault("log.max_backups", 7)
}
