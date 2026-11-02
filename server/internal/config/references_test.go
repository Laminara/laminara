package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func TestReferencesResolveWithoutChangingRawConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"auth":{"provider":"sql","config":{"dsn":"${env:LAMINARA_TEST_DSN}","password":"${file:password}"}}}`
	t.Setenv("LAMINARA_TEST_DSN", `secret with "quotes"`)
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "password"), []byte(" password\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cfg.Auth.Config), `secret with \"quotes\"`) || !strings.Contains(string(cfg.Auth.Config), `" password"`) {
		t.Fatalf("references not resolved correctly")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(after) != raw {
		t.Fatal("raw config changed")
	}
}

func TestMissingReferenceFailsClosed(t *testing.T) {
	_, err := config.Resolve([]byte(`{"secret":"${file:no-such-secret}"}`), filepath.Join(t.TempDir(), "config.json"))
	if err == nil {
		t.Fatal("missing secret accepted")
	}
}
