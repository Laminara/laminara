package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/config"
)

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
		"auth": {
			"provider": "jsonfile",
			"config": { "path": "users.json", "hash": "sha256" },
			"accessTTL": "10m",
			"refreshTTL": "720h",
			"sessions": { "backend": "memory" }
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.Provider != "jsonfile" {
		t.Fatalf("provider = %q", cfg.Auth.Provider)
	}
	if cfg.Auth.AccessTTL.Duration() != 10*time.Minute {
		t.Fatalf("accessTTL = %v", cfg.Auth.AccessTTL.Duration())
	}
	if cfg.Auth.Sessions.Backend != "memory" {
		t.Fatalf("backend = %q", cfg.Auth.Sessions.Backend)
	}
}

func TestShippedExampleLoads(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "..", "deploy", "config.example.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Update.Checks() {
		t.Fatal("example config must leave update checking on")
	}
	if cfg.Update.Installs() {
		t.Fatal("example config must not install updates unattended")
	}
	if cfg.Update.IntervalOr(0) != 24*time.Hour {
		t.Fatalf("interval = %s, want 24h", cfg.Update.IntervalOr(0))
	}
}
