package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaults(t *testing.T) {
	cfg := Defaults()
	if cfg.App.Port != 8080 {
		t.Errorf("default port = %d, want 8080", cfg.App.Port)
	}
	if cfg.Database.Driver != "postgres" {
		t.Errorf("default driver = %q, want postgres", cfg.Database.Driver)
	}
	if cfg.App.GracefulTimeout != 10*time.Second {
		t.Errorf("default graceful timeout = %v, want 10s", cfg.App.GracefulTimeout)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
app:
  env: staging
  port: 9090
log:
  level: debug
  format: console
ratelimit:
  enabled: false
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path, "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.App.Env != "staging" {
		t.Errorf("env = %q, want staging", cfg.App.Env)
	}
	if cfg.App.Port != 9090 {
		t.Errorf("port = %d, want 9090", cfg.App.Port)
	}
	// Values not in the file keep their defaults.
	if cfg.App.Name != "go-web-template" {
		t.Errorf("name = %q, want default", cfg.App.Name)
	}
	if cfg.RateLimit.Enabled {
		t.Error("ratelimit.enabled should be false")
	}
}

func TestLoadEnvOverrides(t *testing.T) {
	t.Setenv("APP_DATABASE_HOST", "db.internal")
	t.Setenv("APP_DATABASE_PORT", "5433")
	t.Setenv("APP_LOG_LEVEL", "warn")
	t.Setenv("APP_CORS_ALLOWED_ORIGINS", "https://a.example,https://b.example")

	// Use a config file that sets conflicting values; env must win.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := `
database:
  host: 127.0.0.1
  port: 5432
log:
  level: info
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path, "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Host != "db.internal" {
		t.Errorf("db host = %q, want env override", cfg.Database.Host)
	}
	if cfg.Database.Port != 5433 {
		t.Errorf("db port = %d, want 5433", cfg.Database.Port)
	}
	if cfg.Log.Level != "warn" {
		t.Errorf("log level = %q, want warn", cfg.Log.Level)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 || cfg.CORS.AllowedOrigins[0] != "https://a.example" {
		t.Errorf("cors origins = %v, want env override", cfg.CORS.AllowedOrigins)
	}
}

func TestLoadExplicitMissingFileFails(t *testing.T) {
	// An explicitly requested config file must fail fast instead of silently
	// falling back to defaults.
	_, err := Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"), "APP")
	if err == nil {
		t.Fatal("expected error when explicit config file is missing")
	}
}

func TestLoadDefaultMissingFileFallsBack(t *testing.T) {
	// When no explicit file is given and configs/config.yaml is absent, the
	// loader must fall back to defaults + env vars.
	cfg, err := Load("", "APP")
	if err != nil {
		t.Fatalf("missing default file should not fail: %v", err)
	}
	if cfg.App.Name != "go-web-template" {
		t.Errorf("expected defaults, got %+v", cfg.App)
	}
}

func TestValidateRejectsBadPort(t *testing.T) {
	cfg := Defaults()
	cfg.App.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for port 0")
	}
}

func TestLoadEnvOverridesAppSection(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("app:\n  port: 8080\n  debug: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// README documents APP_PORT (not APP_APP_PORT): the "app" section must not
	// double the prefix.
	t.Setenv("APP_PORT", "9999")
	t.Setenv("APP_DEBUG", "false")

	cfg, err := Load(path, "APP")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.App.Port != 9999 {
		t.Errorf("port = %d, want env override 9999", cfg.App.Port)
	}
	if cfg.App.Debug {
		t.Error("debug = true, want env override false")
	}
}
