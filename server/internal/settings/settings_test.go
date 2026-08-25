package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadsValuesAndDefaults(t *testing.T) {
	doc, err := Open(write(t, `{"api":{"addr":"127.0.0.1:8099"},"hwid":{"mode":"enforce"}}`))
	if err != nil {
		t.Fatal(err)
	}
	entry, err := doc.Entry("api.addr")
	if err != nil {
		t.Fatal(err)
	}
	if entry.Value != "127.0.0.1:8099" || !entry.IsSet {
		t.Fatalf("entry = %+v", entry)
	}
	missing, err := doc.Entry("api.xAccel")
	if err != nil {
		t.Fatal(err)
	}
	if missing.IsSet || missing.Value != "нет" {
		t.Fatalf("absent field must fall back to its default: %+v", missing)
	}
}

func TestSetValidatesAndSaves(t *testing.T) {
	path := write(t, `{"api":{"addr":"127.0.0.1:8099"}}`)
	doc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("api.xAccel", "да"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("hwid.minScore", "50"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("hwid.hardwareBanTTL", "30d"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("build.trustedSigningKeys", "/a.key, /b.key"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set("hwid.minScore", "много"); err == nil {
		t.Fatal("a word is not a number")
	}
	if err := doc.Set("hwid.mode", "жёстко"); err == nil {
		t.Fatal("only registered modes are allowed")
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		"api.xAccel":               "да",
		"hwid.minScore":            "50",
		"hwid.hardwareBanTTL":      "720h",
		"build.trustedSigningKeys": "/a.key, /b.key",
	} {
		entry, err := reopened.Entry(path)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Value != want {
			t.Fatalf("%s = %q; want %q", path, entry.Value, want)
		}
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatalf("saved config must still load: %v", err)
	}
	if !loaded.API.XAccel || loaded.HWID.MinScore != 50 {
		t.Fatalf("loaded = %+v", loaded.API)
	}
}

func TestEmptyValueRestoresDefault(t *testing.T) {
	path := write(t, `{"hwid":{"minScore":80}}`)
	doc, _ := Open(path)
	if err := doc.Set("hwid.minScore", ""); err != nil {
		t.Fatal(err)
	}
	entry, _ := doc.Entry("hwid.minScore")
	if entry.IsSet || entry.Value != "45" {
		t.Fatalf("clearing a value must fall back to the default: %+v", entry)
	}
}

func TestVariantFollowsProvider(t *testing.T) {
	path := write(t, `{"auth":{"provider":"jsonfile","config":{"path":"/srv/users.json","hash":"argon2id"}}}`)
	doc, _ := Open(path)
	entries, err := doc.Section("auth")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEntry(entries, "auth.config.path") {
		t.Fatalf("jsonfile provider must expose its file: %v", paths(entries))
	}
	if err := doc.Set("auth.provider", "http"); err != nil {
		t.Fatal(err)
	}
	entries, _ = doc.Section("auth")
	if hasEntry(entries, "auth.config.path") || !hasEntry(entries, "auth.config.url") {
		t.Fatalf("switching the provider must switch the fields: %v", paths(entries))
	}
	if _, ok := doc.raw("auth.config.path"); ok {
		t.Fatal("the old provider block must be dropped, not left behind")
	}
}

func TestSecretsAreMasked(t *testing.T) {
	doc, _ := Open(write(t, `{"storage":{"backend":"s3","config":{"secretAccessKey":"very-secret"}}}`))
	entry, err := doc.Entry("storage.config.secretAccessKey")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entry.Value, "very-secret") {
		t.Fatalf("a secret must not be printed: %q", entry.Value)
	}
}

var collectionsEditedElsewhere = map[string]bool{
	"access.sources":       true,
	"access.rules":         true,
	"modules.config":       true,
	"auth.config":          true,
	"storage.config":       true,
	"yggdrasil.skinConfig": true,
	"hwid.store.config":    true,
	"news.source.config":   true,
	"crashReports.sinks":   true,
}

func TestSchemaCoversEveryConfigField(t *testing.T) {
	known := map[string]bool{}
	for _, section := range Sections() {
		for _, field := range section.Fields {
			known[section.Key+"."+field.Key] = true
		}
	}
	var walk func(prefix string, kind reflect.Type)
	walk = func(prefix string, kind reflect.Type) {
		for kind.Kind() == reflect.Ptr {
			kind = kind.Elem()
		}
		if kind.Kind() != reflect.Struct {
			return
		}
		for i := 0; i < kind.NumField(); i++ {
			field := kind.Field(i)
			tag, _, _ := strings.Cut(field.Tag.Get("json"), ",")
			if tag == "" || tag == "-" {
				continue
			}
			path := tag
			if prefix != "" {
				path = prefix + "." + tag
			}
			if collectionsEditedElsewhere[path] {
				continue
			}
			inner := field.Type
			for inner.Kind() == reflect.Ptr {
				inner = inner.Elem()
			}
			if inner.Kind() == reflect.Struct && inner.Name() != "Duration" && inner.PkgPath() != "time" {
				walk(path, inner)
				continue
			}
			if !known[path] {
				t.Errorf("настройка %s есть в конфиге, но её нет в схеме — оператор её не отредактирует", path)
			}
		}
	}
	walk("", reflect.TypeOf(config.Config{}))
}

func TestSchemaHasNoStrayPaths(t *testing.T) {
	body, err := json.Marshal(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if len(body) == 0 {
		t.Fatal("config must marshal")
	}
	for _, section := range Sections() {
		for _, field := range section.Fields {
			if field.Label == "" {
				t.Errorf("%s.%s без подписи", section.Key, field.Key)
			}
			if field.Kind == KindChoice && len(field.options()) == 0 {
				t.Errorf("%s.%s — выбор без вариантов", section.Key, field.Key)
			}
		}
	}
}

func hasEntry(entries []Entry, path string) bool {
	for _, entry := range entries {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func paths(entries []Entry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Path)
	}
	return out
}

func TestAccessRulesCollection(t *testing.T) {
	path := write(t, `{"access":{"sources":{"closed":{"type":"file","config":{"path":"/srv/list.json"}}},"rules":[{"builds":["test-*"],"source":"closed","visibility":"hidden","message":"Закрытый тест"}]}}`)
	doc, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	collections, err := doc.Collections("access")
	if err != nil {
		t.Fatal(err)
	}
	if len(collections) != 2 || len(collections[0].Entries) != 1 || collections[0].Entries[0].Title != "closed" {
		t.Fatalf("collections = %+v", collections)
	}
	if collections[1].Entries[0].Title != "test-*" {
		t.Fatalf("a rule must be named by the builds it covers: %+v", collections[1].Entries[0])
	}

	fields, err := doc.EntryFields("access.sources.closed")
	if err != nil {
		t.Fatal(err)
	}
	if !hasEntry(fields, "access.sources.closed.config.path") {
		t.Fatalf("a file source must expose its file: %v", paths(fields))
	}

	added, err := doc.AddEntry("access.rules", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set(added+".builds", "survival, creative"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set(added+".source", "closed"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Access.Rules) != 2 || loaded.Access.Rules[1].Builds[1] != "creative" {
		t.Fatalf("rules = %+v", loaded.Access.Rules)
	}

	if err := doc.RemoveEntry("access.rules.0"); err != nil {
		t.Fatal(err)
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, _ = config.Load(path)
	if len(loaded.Access.Rules) != 1 || loaded.Access.Rules[0].Source != "closed" {
		t.Fatalf("after removal: %+v", loaded.Access.Rules)
	}

	if _, err := doc.AddEntry("access.sources", "closed"); err == nil {
		t.Fatal("a duplicate name must be refused")
	}
	if _, err := doc.AddEntry("access.sources", "с точкой.в имени"); err == nil {
		t.Fatal("a name with a dot must be refused")
	}
}

func TestModuleConfigIsRawJSON(t *testing.T) {
	path := write(t, `{"modules":{"dir":"/srv/modules"}}`)
	doc, _ := Open(path)
	entryPath, err := doc.AddEntry("modules.config", "greeter")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Set(entryPath, `{"channel":"общий"}`); err != nil {
		t.Fatal(err)
	}
	if err := doc.Set(entryPath, "не json"); err == nil {
		t.Fatal("broken JSON must be refused")
	}
	if err := doc.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]string
	if err := json.Unmarshal(loaded.Modules.Config["greeter"], &stored); err != nil {
		t.Fatal(err)
	}
	if stored["channel"] != "общий" {
		t.Fatalf("module config = %s", loaded.Modules.Config["greeter"])
	}
}
