package daemon

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/settings"
)

func settingsRun(t *testing.T, store *settingsStore, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := settingsCommand(store).Run(context.Background(), args, &out); err != nil {
		t.Fatalf("settings %s: %v", strings.Join(args, " "), err)
	}
	return out.String()
}

func settingsStoreFor(t *testing.T) *settingsStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &settingsStore{path: path}
}

func configureAuth(t *testing.T, store *settingsStore, provider string, changes ...*adminv1.SettingChange) {
	t.Helper()
	page, err := store.List("", "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.Apply(&adminv1.SettingsPatch{Revision: page.Revision, Changes: append([]*adminv1.SettingChange{settingChange("auth.provider", provider)}, changes...)})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSettingsEntryPrintsOneBlockForALeafSetting(t *testing.T) {
	store := settingsStoreFor(t)

	out := settingsRun(t, store, "api.addr")
	if !strings.Contains(out, "Адрес и порт") || !strings.Contains(out, "значение:") {
		t.Fatalf("a leaf setting needs the detailed block, got %q", out)
	}
	if !strings.Contains(out, "по умолчанию: 0.0.0.0:8099") {
		t.Fatalf("the default must be shown, got %q", out)
	}
}

func TestSettingsEntryPrintsRowsForACollectionItem(t *testing.T) {
	store := settingsStoreFor(t)
	page, err := store.List("", "")
	if err != nil {
		t.Fatal(err)
	}
	patch := &adminv1.SettingsPatch{
		Revision: page.Revision,
		Changes: []*adminv1.SettingChange{
			{Action: &adminv1.SettingChange_Add{Add: &adminv1.AddSettingEntryRequest{Collection: "access.sources", Name: "staff"}}},
			settingChange("access.sources.staff.type", "file"),
			settingChange("access.sources.staff.config.path", filepath.Join(t.TempDir(), "staff.json")),
			{Action: &adminv1.SettingChange_Add{Add: &adminv1.AddSettingEntryRequest{Collection: "access.rules"}}},
			settingChange("access.rules.0.builds", "TestServer"),
			settingChange("access.rules.0.source", "staff"),
		},
	}
	preview, err := store.Preview(&adminv1.PreviewSettingsRequest{Patch: patch, Entry: "access.rules.0"})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ValidationError != "" || preview.AddedPath != "access.rules.0" {
		t.Fatalf("completed access rule rejected: %+v", preview)
	}
	if readFile(t, store.path) != "{}" {
		t.Fatal("preview persisted the access rule")
	}
	if _, err := store.Apply(patch); err != nil {
		t.Fatal(err)
	}

	out := settingsRun(t, store, "access.rules.0")
	if strings.Contains(out, "значение:") {
		t.Fatalf("a list item must print its fields as rows, got %q", out)
	}
	for _, want := range []string{"Сборки", "access.rules.0.builds", "Видимость", "access.rules.0.visibility"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %q", want, out)
		}
	}
}

func TestSettingsCannotSaveAnIncompleteAccessRule(t *testing.T) {
	store := settingsStoreFor(t)
	before := readFile(t, store.path)
	var out bytes.Buffer
	err := settingsCommand(store).Run(context.Background(), []string{"add", "access.rules"}, &out)
	if err == nil || !strings.Contains(err.Error(), "access.rules") {
		t.Fatalf("incomplete rule must be rejected with its setting path, got %v", err)
	}
	if readFile(t, store.path) != before || store.pending || strings.Contains(out.String(), "Добавлено:") {
		t.Fatal("rejected access rule was saved or reported as added")
	}
}

func TestSettingsWriteLandsInTheFileAndCanBeCleared(t *testing.T) {
	store := settingsStoreFor(t)

	out := settingsRun(t, store, "api.addr", "127.0.0.1:9000")
	if !strings.Contains(out, "Адрес и порт: 127.0.0.1:9000") {
		t.Fatalf("the written value must be echoed, got %q", out)
	}
	data, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "127.0.0.1:9000") {
		t.Fatalf("the value must reach the file, got %s", data)
	}

	out = settingsRun(t, store, "clear", "api.addr")
	if !strings.Contains(out, "0.0.0.0:8099 (по умолчанию)") {
		t.Fatalf("a cleared setting must fall back to its default, got %q", out)
	}
	data, err = os.ReadFile(store.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "127.0.0.1:9000") {
		t.Fatalf("clearing must remove the value from the file, got %s", data)
	}
}

func TestSettingsWithoutAFileRefuses(t *testing.T) {
	store := &settingsStore{path: filepath.Join(t.TempDir(), "absent.json")}

	var out bytes.Buffer
	if err := settingsCommand(store).Run(context.Background(), []string{"api.addr"}, &out); err == nil {
		t.Fatal("a missing config file must be an error, not silence")
	}
}

func TestSettingsNeverEchoASecret(t *testing.T) {
	store := settingsStoreFor(t)
	const secret = "postgres://operator:p@ssw0rd@db:5432/game"

	configureAuth(t, store, "sql", settingChange("auth.config.driver", "postgres"), settingChange("auth.config.dsn", "postgres://operator@db/game"))
	echo := settingsRun(t, store, "auth.config.dsn", secret)
	if strings.Contains(echo, secret) {
		t.Fatalf("the echo of a written secret must be masked, got %q", echo)
	}
	if !strings.Contains(echo, settings.SecretMask) {
		t.Fatalf("the echo must show the mask, got %q", echo)
	}
	if !strings.Contains(string(readFile(t, store.path)), secret) {
		t.Fatalf("the secret itself must still reach the file: %s", readFile(t, store.path))
	}

	for _, args := range [][]string{{"auth.config.dsn"}, {"auth.config"}, {"auth"}} {
		shown := settingsRun(t, store, args...)
		if strings.Contains(shown, secret) {
			t.Fatalf("settings %s must not print the secret, got %q", strings.Join(args, " "), shown)
		}
		if !strings.Contains(shown, settings.SecretMask) {
			t.Fatalf("settings %s must show the mask, got %q", strings.Join(args, " "), shown)
		}
	}
}

func TestSettingsEchoForACollectionSecretIsMasked(t *testing.T) {
	store := settingsStoreFor(t)
	const hook = "https://discord/app/webhook/SECRET"

	settingsRun(t, store, "add", "crashReports.sinks", "hook")
	settingsRun(t, store, "crashReports.sinks.hook.type", "discord")
	echo := settingsRun(t, store, "crashReports.sinks.hook.config.url", hook)

	if strings.Contains(echo, hook) {
		t.Fatalf("the echo of a written webhook must be masked, got %q", echo)
	}
	if !strings.Contains(echo, settings.SecretMask) {
		t.Fatalf("the echo must show the mask, got %q", echo)
	}
	shown := settingsRun(t, store, "crashReports.sinks.hook")
	if strings.Contains(shown, hook) || !strings.Contains(shown, settings.SecretMask) {
		t.Fatalf("the entry view must keep the secret masked, got %q", shown)
	}
}

func TestSettingsScreenKeepsEditingASecret(t *testing.T) {
	store := settingsStoreFor(t)
	configureAuth(t, store, "sql", settingChange("auth.config.driver", "postgres"), settingChange("auth.config.dsn", "postgres://operator@db/game"))
	settingsRun(t, store, "auth.config.dsn", "postgres://operator:secret@db:5432/game")

	entry, err := store.Entry("auth.config.dsn")
	if err != nil {
		t.Fatal(err)
	}
	if entry.entry.Value != settings.SecretMask {
		t.Fatalf("the screen must show the mask, got %q", entry.entry.Value)
	}
	if entry.entry.Kind != string(settings.KindSecret) {
		t.Fatalf("the screen must keep the kind so the editor starts empty, got %q", entry.entry.Kind)
	}

	shown := settingsRun(t, store, "auth.config.dsn", "postgres://other:s3cret@db:5432/game")
	if strings.Contains(shown, "postgres://other") || strings.Contains(shown, "s3cret") {
		t.Fatalf("rewriting a secret must be masked too, got %q", shown)
	}
	if !strings.Contains(string(readFile(t, store.path)), "postgres://other") {
		t.Fatal("the new value must reach the file")
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWholeSettingWritesNeverEchoSecrets(t *testing.T) {
	store := settingsStoreFor(t)
	settingsRun(t, store, "add", "crashReports.sinks", "hook")
	for _, test := range []struct{ path, value, secret string }{
		{"crashReports.sinks.hook", `{"type":"discord","config":{"url":"https://discord/SECRET","username":"bot"}}`, "https://discord/SECRET"},
		{"modules.config.custom", `{"arbitrary":["OPAQUE-SECRET"]}`, "OPAQUE-SECRET"},
	} {
		output := settingsRun(t, store, test.path, test.value)
		if strings.Contains(output, test.secret) || !strings.Contains(output, settings.SecretMask) {
			t.Fatalf("whole setting write echoed a secret: %s", test.path)
		}
		if !strings.Contains(readFile(t, store.path), test.secret) {
			t.Fatalf("whole setting write lost its secret: %s", test.path)
		}
	}
}

func TestRedactedEditPreservesTheLatestStoredHeader(t *testing.T) {
	store := settingsStoreFor(t)
	configureAuth(t, store, "http", settingChange("auth.config.url", "https://example.test/auth"))
	settingsRun(t, store, "auth.config.headers", `{"Authorization":"old","X-Label":"one"}`)
	page, err := store.List("auth", "")
	if err != nil || len(page.Entries) == 0 {
		t.Fatalf("settings page: %v", err)
	}
	settingsRun(t, store, "auth.config.headers", `{"Authorization":"latest-secret","X-Label":"one"}`)
	entry, err := store.Edit("auth.config.headers", `{"Authorization":"***","X-Label":"два, три=4"}`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(entry.Value, "latest-secret") || !strings.Contains(readFile(t, store.path), "latest-secret") || !strings.Contains(readFile(t, store.path), "два, три=4") {
		t.Fatal("edit did not preserve the latest secret with the new public value")
	}
}
