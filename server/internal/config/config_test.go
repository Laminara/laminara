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

func TestDurationsParseTheSameEverywhere(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
		"auth": { "accessTTL": "30d" },
		"hwid": { "ticketTTL": "30d" },
		"rateLimit": { "login": { "limit": 10, "per": "30d" } }
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	day := 30 * 24 * time.Hour
	if cfg.Auth.AccessTTL.Duration() != day {
		t.Fatalf("auth.accessTTL = %s, want %s", cfg.Auth.AccessTTL, day)
	}
	if cfg.HWID.TicketTTL.Duration() != day {
		t.Fatalf("hwid.ticketTTL = %s, want %s", cfg.HWID.TicketTTL, day)
	}
	if cfg.RateLimit.Login.Per.Duration() != day {
		t.Fatalf("rateLimit.login.per = %s, want %s", cfg.RateLimit.Login.Per, day)
	}
}

func TestDurationGarbageIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"auth": { "accessTTL": "позже" }}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := config.Load(path); err == nil {
		t.Fatal("a word is not a duration")
	}
}
