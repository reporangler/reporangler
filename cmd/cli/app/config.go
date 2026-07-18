package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Endpoints holds the base URLs of every service the CLI talks to.
type Endpoints struct {
	Auth     string `json:"auth"`
	Metadata string `json:"metadata"`
	PHP      string `json:"php"`
	NPM      string `json:"npm"`
	Storage  string `json:"storage"`
}

// Config is the on-disk CLI configuration. It matches the legacy shape
// {endpoints,login_token,user_id}. The bearer token is persisted here after a
// successful login and sent on every authenticated request.
type Config struct {
	Endpoints  Endpoints `json:"endpoints"`
	LoginToken string    `json:"login_token"`
	UserID     int64     `json:"user_id"`
}

// DefaultConfig returns a Config pointed at the local docker-compose stack
// (http://<svc>.reporangler.localhost).
func DefaultConfig() Config {
	return Config{
		Endpoints: Endpoints{
			Auth:     "http://auth.reporangler.localhost",
			Metadata: "http://metadata.reporangler.localhost",
			PHP:      "http://php.reporangler.localhost",
			NPM:      "http://npm.reporangler.localhost",
			Storage:  "http://storage.reporangler.localhost",
		},
	}
}

// ConfigPath returns the path to the CLI config file, honouring
// $XDG_CONFIG_HOME and falling back to ~/.config.
func ConfigPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "reporangler", "config.json"), nil
}

// LoadConfig reads the config file at path. If the file does not exist a
// default config is returned (and no error). Any endpoint left empty by the
// file is backfilled with its default so a partial file still works.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	// Start from an empty config so unset fields stay zero, then apply defaults
	// for any endpoint the file omitted.
	var loaded Config
	if err := json.Unmarshal(raw, &loaded); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	def := DefaultConfig()
	if loaded.Endpoints.Auth == "" {
		loaded.Endpoints.Auth = def.Endpoints.Auth
	}
	if loaded.Endpoints.Metadata == "" {
		loaded.Endpoints.Metadata = def.Endpoints.Metadata
	}
	if loaded.Endpoints.PHP == "" {
		loaded.Endpoints.PHP = def.Endpoints.PHP
	}
	if loaded.Endpoints.NPM == "" {
		loaded.Endpoints.NPM = def.Endpoints.NPM
	}
	if loaded.Endpoints.Storage == "" {
		loaded.Endpoints.Storage = def.Endpoints.Storage
	}
	return loaded, nil
}

// SaveConfig writes cfg to path, creating parent directories as needed. It only
// touches the file when the serialized content differs from what is already on
// disk, so repeated no-op runs don't rewrite the file. It reports whether a
// write happened.
func SaveConfig(path string, cfg Config) (bool, error) {
	out, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return false, err
	}
	out = append(out, '\n')

	if existing, err := os.ReadFile(path); err == nil {
		if string(existing) == string(out) {
			return false, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return false, err
	}
	return true, nil
}
