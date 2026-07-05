package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfigHasLocalhostEndpoints(t *testing.T) {
	c := DefaultConfig()
	eps := map[string]string{
		"auth":     c.Endpoints.Auth,
		"metadata": c.Endpoints.Metadata,
		"php":      c.Endpoints.PHP,
		"npm":      c.Endpoints.NPM,
		"storage":  c.Endpoints.Storage,
	}
	for name, url := range eps {
		if !strings.HasPrefix(url, "http://") || !strings.Contains(url, "reporangler.localhost") {
			t.Errorf("endpoint %s = %q, want http://<svc>.reporangler.localhost", name, url)
		}
	}
	if c.LoginToken != "" || c.UserID != 0 {
		t.Errorf("default config should have no token/user, got %q/%d", c.LoginToken, c.UserID)
	}
}

func TestConfigPathXDG(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/custom/cfg")
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := "/custom/cfg/reporangler/config.json"; got != want {
		t.Errorf("ConfigPath() = %q, want %q", got, want)
	}
}

func TestConfigPathFallback(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got, filepath.Join(".config", "reporangler", "config.json")) {
		t.Errorf("ConfigPath() = %q, want a ~/.config/reporangler/config.json path", got)
	}
}

func TestLoadConfigMissingReturnsDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope", "config.json")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if cfg.Endpoints.Auth != DefaultConfig().Endpoints.Auth {
		t.Errorf("missing file should yield defaults, got %q", cfg.Endpoints.Auth)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	in := DefaultConfig()
	in.LoginToken = "abc123"
	in.UserID = 7
	if _, err := SaveConfig(path, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if out.LoginToken != "abc123" || out.UserID != 7 {
		t.Errorf("round-trip lost data: %+v", out)
	}
	if out.Endpoints != in.Endpoints {
		t.Errorf("endpoints changed: %+v vs %+v", out.Endpoints, in.Endpoints)
	}
}

func TestSaveOnlyWritesOnChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultConfig()
	cfg.LoginToken = "t1"

	changed, err := SaveConfig(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("first save should report a change")
	}

	// Saving identical content must be a no-op and not rewrite the file.
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	changed, err = SaveConfig(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("saving identical config should report no change")
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("file was rewritten despite no change")
	}

	// A real change writes again.
	cfg.LoginToken = "t2"
	changed, err = SaveConfig(path, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("changed config should be written")
	}
}

func TestLoadBackfillsMissingEndpoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	// A file that only sets a token/user and one endpoint.
	raw := `{"endpoints":{"auth":"http://auth.example.test"},"login_token":"tok","user_id":3}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoints.Auth != "http://auth.example.test" {
		t.Errorf("explicit endpoint overwritten: %q", cfg.Endpoints.Auth)
	}
	if cfg.Endpoints.Metadata != DefaultConfig().Endpoints.Metadata {
		t.Errorf("missing endpoint not backfilled: %q", cfg.Endpoints.Metadata)
	}
	if cfg.LoginToken != "tok" || cfg.UserID != 3 {
		t.Errorf("token/user not loaded: %+v", cfg)
	}
}
