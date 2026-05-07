// Package clicred manages the operator CLI's local credentials file:
// the small YAML blob written by `quicktun login` and read by every
// subsequent CLI command. The file lives at ~/.config/quicktun/credentials.yaml
// by default, mode 0o600, parent dir 0o700 — so other users on a shared
// host cannot read the operator's bearer token.
package clicred

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Credentials is the on-disk shape of credentials.yaml. The CLI loads it
// at startup, mutates fields during commands like `login` / `logout` /
// `set-context`, and writes it back atomically via Save.
type Credentials struct {
	// Endpoint is the gRPC control plane address (host:port).
	Endpoint string `yaml:"endpoint"`
	// AuthProxyEndpoint is the operator-facing auth-proxy address
	// (host:port) recorded at login time so subsequent commands that
	// need it (e.g. tunnel) don't have to re-prompt.
	AuthProxyEndpoint string `yaml:"auth_proxy_endpoint"`
	// SessionToken is the raw access token returned by AuthService.Login.
	// Presented as `Authorization: Bearer <token>` on every RPC.
	SessionToken string `yaml:"session_token"`
	// OperatorEmail is recorded for human-readable display
	// (`Logged in as ...`) and never sent over the wire.
	OperatorEmail string `yaml:"operator_email"`
	// TLSInsecure disables TLS verification on the gRPC dial AND lets
	// the bearer-token PerRPCCredentials work over a non-TLS transport.
	// Dev-only mirror of agent.Config.TLSInsecure.
	TLSInsecure bool `yaml:"tls_insecure"`
}

// DefaultPath resolves the credentials path. Order of precedence:
//
//  1. $QUICKTUN_CONFIG (full path, including filename).
//  2. $XDG_CONFIG_HOME/quicktun/credentials.yaml.
//  3. ~/.config/quicktun/credentials.yaml.
//
// Returns an error only when neither $XDG_CONFIG_HOME nor a usable
// home directory can be resolved.
func DefaultPath() (string, error) {
	if v := os.Getenv("QUICKTUN_CONFIG"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "quicktun", "credentials.yaml"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("clicred: resolve home dir: %w", err)
	}
	return filepath.Join(home, ".config", "quicktun", "credentials.yaml"), nil
}

// Load reads and parses the credentials file at path. A missing file is
// surfaced as an *fs.PathError wrapping os.ErrNotExist so callers can
// detect "no login yet" via errors.Is(err, os.ErrNotExist).
func Load(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		// os.ReadFile already returns *fs.PathError; preserve it so
		// errors.Is(err, fs.ErrNotExist) works for callers.
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			return nil, pathErr
		}
		return nil, fmt.Errorf("clicred: read %s: %w", path, err)
	}
	var c Credentials
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("clicred: parse %s: %w", path, err)
	}
	return &c, nil
}

// Save serializes c to YAML and writes it to path with mode 0o600,
// creating the parent directory at 0o700 if missing. The write goes
// through a temp file + rename so a crash mid-write cannot leave the
// credentials file truncated.
func Save(path string, c *Credentials) error {
	if c == nil {
		return fmt.Errorf("clicred: nil credentials")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("clicred: mkdir %s: %w", dir, err)
	}
	// MkdirAll skips chmod when the directory already exists with a
	// looser mode (e.g. ~/.config at 0o755 on most systems). We only
	// tighten the directory we own — the leaf one — so we don't fight
	// the user's umask on intermediate paths like ~/.config.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("clicred: chmod %s: %w", dir, err)
	}

	data, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("clicred: marshal: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".credentials-*.yaml.tmp")
	if err != nil {
		return fmt.Errorf("clicred: create temp: %w", err)
	}
	tmpPath := tmp.Name()
	// Best-effort cleanup on error; ignored once rename succeeds.
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("clicred: chmod temp: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("clicred: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("clicred: close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("clicred: rename: %w", err)
	}
	return nil
}
