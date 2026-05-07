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
