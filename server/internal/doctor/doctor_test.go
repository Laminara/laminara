package doctor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/serversetup"
)

func TestEverySettingSectionIsChecked(t *testing.T) {
	covered := map[string]bool{}
	for _, key := range Sections() {
		covered[key] = true
	}
	fields := reflect.TypeOf(config.Config{})
	for i := 0; i < fields.NumField(); i++ {
		tag := fields.Field(i).Tag.Get("json")
		name, _, _ := strings.Cut(tag, ",")
		if name == "" || name == "-" {
			continue
		}
		if !covered[name] {
			t.Errorf("раздел конфига %q никто не проверяет — добавьте секцию в doctor", name)
		}
	}
}

func TestEverySectionHasTitle(t *testing.T) {
	titles := Titles()
	for _, key := range Sections() {
		if strings.TrimSpace(titles[key]) == "" {
			t.Errorf("у секции %q нет заголовка", key)
		}
	}
}

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func minimalConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	users := write(t, filepath.Join(dir, "users.json"), `[{"username":"Steve","password":"$2a$12$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ012"}]`)
	body := map[string]any{
		"auth": map[string]any{
			"provider": "jsonfile",
			"config":   map[string]any{"path": users, "hash": "bcrypt"},
		},
		"storage": map[string]any{
			"backend": "fs",
			"config":  map[string]any{"root": filepath.Join(dir, "objects")},
		},
		"build": map[string]any{
			"profilesDir":    filepath.Join(dir, "profiles"),
			"signingKeyPath": filepath.Join(dir, "signing.key"),
		},
		"api":       map[string]any{"addr": "127.0.0.1:0"},
		"yggdrasil": map[string]any{"enabled": true, "skinProvider": "template", "skinConfig": map[string]any{"skin": "https://skins.example.com/%nickname%.png"}},
		"console":   map[string]any{"enabled": false},
		"update":    map[string]any{"check": false},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	path := write(t, filepath.Join(dir, "config.json"), string(encoded))
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, path
}

func run(t *testing.T, cfg *config.Config, path string, only ...string) []diag.Result {
	t.Helper()
	wired, err := serversetup.Build(cfg)
	opts := Options{Config: cfg, ConfigPath: path, Wired: wired, BuildError: err}
	return RunSections(context.Background(), opts, only)
}

func find(results []diag.Result, what string) (diag.Result, bool) {
	for _, result := range results {
		if strings.HasPrefix(result.What, what) {
			return result, true
		}
	}
	return diag.Result{}, false
}

func TestStorageRoundtripPassesAndCleansUp(t *testing.T) {
	cfg, path := minimalConfig(t)
	results := run(t, cfg, path, "storage")
	result, ok := find(results, "заливка")
	if !ok {
		t.Fatal("проверки заливки не было")
	}
	if result.Verdict != diag.OK {
		t.Fatalf("заливка не прошла: %s", result.Detail)
	}
	root := filepath.Join(filepath.Dir(path), "objects")
	entries, err := os.ReadDir(filepath.Join(root, "doctor"))
	if err == nil && len(entries) > 0 {
		t.Fatalf("пробный объект остался в хранилище: %d шт", len(entries))
	}
}

func TestHashSchemeMismatchIsFatal(t *testing.T) {
	cfg, path := minimalConfig(t)
	var provider struct {
		Path string `json:"path"`
		Hash string `json:"hash"`
	}
	if err := json.Unmarshal(cfg.Auth.Config, &provider); err != nil {
		t.Fatal(err)
	}
	provider.Hash = "argon2id"
	encoded, err := json.Marshal(provider)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Auth.Config = encoded

	results := run(t, cfg, path, "auth")
	result, ok := find(results, "формат паролей")
	if !ok {
		t.Fatal("схему хранения паролей никто не проверил")
	}
	if result.Verdict != diag.Fail {
		t.Fatalf("расхождение схемы должно быть фатальным, получили %v: %s", result.Verdict, result.Detail)
	}
	if !strings.Contains(result.Remedy.Command, "bcrypt") {
		t.Fatalf("подсказка должна предлагать реальную схему, получили %q", result.Remedy.Command)
	}
}

func TestYggdrasilOffIsFatal(t *testing.T) {
	cfg, path := minimalConfig(t)
	cfg.Yggdrasil.Enabled = false
	results := run(t, cfg, path, "yggdrasil")
	if diag.Worst(results) != diag.Fail {
		t.Fatal("выключенный yggdrasil должен быть фатальным — без него не войдёт никто")
	}
}

func TestSkinDomainMissingIsCaught(t *testing.T) {
	cfg, path := minimalConfig(t)
	results := run(t, cfg, path, "consistency")
	result, ok := find(results, "домены скинов")
	if !ok {
		t.Fatal("домены скинов никто не сверил")
	}
	if result.Verdict != diag.Fail {
		t.Fatalf("домен скинов вне skinDomains должен быть фатальным, получили %v", result.Verdict)
	}

	cfg.Yggdrasil.SkinDomains = []string{"skins.example.com"}
	results = run(t, cfg, path, "consistency")
	result, _ = find(results, "домены скинов")
	if result.Verdict != diag.OK {
		t.Fatalf("совпадающий домен должен проходить, получили %v: %s", result.Verdict, result.Detail)
	}
}

func TestEnforceWithMemoryStoreIsFatal(t *testing.T) {
	cfg, path := minimalConfig(t)
	cfg.HWID = &hwid.Config{Mode: hwid.ModeEnforce}
	cfg.HWID.Store.Backend = "memory"
	results := run(t, cfg, path, "consistency")
	result, ok := find(results, "хранение банов")
	if !ok {
		t.Fatal("сочетание enforce и памяти никто не заметил")
	}
	if result.Verdict != diag.Fail {
		t.Fatalf("баны, исчезающие при перезапуске, — это фатально, получили %v", result.Verdict)
	}
}

func TestUnknownSectionIsReported(t *testing.T) {
	cfg, path := minimalConfig(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	body["storge"] = map[string]any{"backend": "fs"}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	write(t, path, string(encoded))

	results := run(t, cfg, path, "config")
	result, ok := find(results, "незнакомые разделы")
	if !ok {
		t.Fatal("опечатка в имени раздела прошла незамеченной")
	}
	if !strings.Contains(result.Detail, "storge") {
		t.Fatalf("в тексте нет самой опечатки: %s", result.Detail)
	}
}

func TestFixRepairsPermissions(t *testing.T) {
	cfg, path := minimalConfig(t)
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	results := run(t, cfg, path, "config")
	result, ok := find(results, "права на настройки")
	if !ok {
		t.Fatal("открытые права на конфиг не замечены")
	}
	if result.Remedy.Apply == nil {
		t.Fatal("права должны чиниться сами")
	}
	if err := result.Remedy.Apply(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("после починки права остались %v", info.Mode().Perm())
	}
}
