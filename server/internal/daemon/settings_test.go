package daemon

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	settingsRun(t, store, "add", "access.rules")

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

	settingsRun(t, store, "auth.provider", "sql")
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
	settingsRun(t, store, "auth.provider", "sql")
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
