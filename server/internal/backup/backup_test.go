package backup_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/backup"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/hwid"
)

func stand(t *testing.T) (*config.Config, string, string) {
	t.Helper()
	dir := t.TempDir()

	write := func(name, body string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}

	signing := write("signing.hex", "ключ подписи")
	retired := write("retired.hex", "отставной ключ")
	rsa := write("yggdrasil.pem", "rsa")
	salt := write("salt.hex", "соль")
	machines := write("machines.db", "sqlite")
	configPath := write("config.json", "{}")

	cfg := &config.Config{
		Build:     &config.BuildConfig{SigningKeyPath: signing, TrustedSigningKeys: []string{retired, "aa"}, ProfilesDir: filepath.Join(dir, "profiles")},
		Yggdrasil: &config.YggdrasilConfig{RSAKeyPath: rsa},
	}
	store, err := json.Marshal(map[string]string{"driver": "sqlite", "dsn": machines})
	if err != nil {
		t.Fatal(err)
	}
	cfg.HWID = &hwid.Config{
		SaltPath: salt,
		Store:    hwid.StoreConfig{Backend: "sql", Config: store},
	}
	return cfg, configPath, dir
}

func TestBackupTakesEverythingIrreplaceable(t *testing.T) {
	cfg, configPath, dir := stand(t)
	archive := filepath.Join(dir, "backup.tar.gz")

	manifest, err := backup.Create(cfg, configPath, archive)
	if err != nil {
		t.Fatal(err)
	}

	want := map[string]bool{
		"настройки сервера":        false,
		"ключ подписи сборок":      false,
		"отставной ключ подписи":   false,
		"ключ входа в игре":        false,
		"соль отпечатков":          false,
		"база компьютеров и банов": false,
	}
	for _, item := range manifest.Items {
		if _, ok := want[item.What]; ok {
			want[item.What] = true
		}
	}
	for what, taken := range want {
		if !taken {
			t.Errorf("%s не попал в сохранение", what)
		}
	}
}

func TestBackupNamesWhatItLeftOut(t *testing.T) {
	cfg, configPath, dir := stand(t)

	manifest, err := backup.Create(cfg, configPath, filepath.Join(dir, "backup.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Skipped) == 0 {
		t.Fatal("сохранение обязано честно перечислить, чего в нём нет")
	}
	if !strings.Contains(strings.Join(manifest.Skipped, " "), "сборки") {
		t.Fatalf("skipped = %v", manifest.Skipped)
	}
}

func TestRestorePutsFilesBack(t *testing.T) {
	cfg, configPath, dir := stand(t)
	archive := filepath.Join(dir, "backup.tar.gz")
	if _, err := backup.Create(cfg, configPath, archive); err != nil {
		t.Fatal(err)
	}

	into := t.TempDir()
	results, err := backup.Restore(archive, into, false)
	if err != nil {
		t.Fatal(err)
	}

	written := 0
	for _, result := range results {
		if !result.Written {
			t.Errorf("%s не восстановлен: %s", result.Item.Path, result.Reason)
			continue
		}
		written++
		if _, err := os.Stat(result.Item.Path); err != nil {
			t.Errorf("%s обещан восстановленным, но его нет", result.Item.Path)
		}
	}
	if written == 0 {
		t.Fatal("не восстановлено ни одного файла")
	}
}

func TestRestoreDoesNotOverwriteAKeyByAccident(t *testing.T) {
	cfg, configPath, dir := stand(t)
	archive := filepath.Join(dir, "backup.tar.gz")
	if _, err := backup.Create(cfg, configPath, archive); err != nil {
		t.Fatal(err)
	}

	results, err := backup.Restore(archive, "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, result := range results {
		if result.Written {
			t.Fatalf("%s перезаписан, хотя файл был на месте", result.Item.Path)
		}
	}

	body, err := os.ReadFile(cfg.Build.SigningKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ключ подписи" {
		t.Fatal("живой ключ подписи изменился")
	}
}

func TestRestoreRefusesSomethingThatIsNotABackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "random.tar.gz")
	if err := os.WriteFile(path, []byte("не архив"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := backup.Restore(path, "", false); err == nil {
		t.Fatal("посторонний файл не должен приниматься за сохранение")
	}
}
