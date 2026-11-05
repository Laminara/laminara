package settings

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func TestAccountPolicyCanBeEditedFromSettings(t *testing.T) {
	path := write(t, `{}`)
	doc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"launcher.multiAccount": "да", "launcher.maxAccounts": "3"} {
		if err := doc.Set(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg config.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Launcher.MultiAccount || cfg.Launcher.AccountLimit() != 3 {
		t.Fatalf("policy not saved: %+v", cfg.Launcher)
	}
	if err := doc.Set("launcher.maxAccounts", "21"); err == nil {
		if err := doc.Save(); err == nil {
			t.Fatal("out of range policy saved")
		}
	}
	unchanged, err := os.ReadFile(path)
	if err != nil || string(unchanged) != string(data) {
		t.Fatal("invalid settings changed the file")
	}
}

func TestHTTPAccessSettingsUseSupportedModes(t *testing.T) {
	doc, err := Open(write(t, `{"access":{"sources":{"site":{"type":"http","config":{"url":"https://site.example/access"}}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"list", "check"} {
		if err := doc.Set("access.sources.site.config.mode", mode); err != nil {
			t.Fatalf("%s: %v", mode, err)
		}
		if err := doc.Save(); err != nil {
			t.Fatal(err)
		}
	}
	if err := doc.Set("access.sources.site.config.mode", "ask"); err == nil {
		t.Fatal("unsupported mode accepted")
	}
}
