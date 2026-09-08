package tempdir_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/tempdir"
)

func TestFallbackNeverLandsInsideBuildsDir(t *testing.T) {
	cfg := &config.Config{
		Build:    &config.BuildConfig{ProfilesDir: "/var/lib/laminara/profiles", SigningKeyPath: "/var/lib/laminara/signing.key"},
		Storage:  &config.StorageConfig{Backend: "fs", Config: json.RawMessage(`{"root":"/var/lib/laminara/objects"}`)},
		Launcher: &config.LauncherConfig{Dir: "/var/lib/laminara/launcher"},
	}
	for _, dir := range tempdir.Candidates(cfg, "/etc/laminara/config.json") {
		if dir == cfg.Build.ProfilesDir {
			t.Fatal("временный каталог оказался бы среди сборок, и команда builds показала бы его как сборку")
		}
	}
}

func TestFallbackPicksFirstWritableCandidate(t *testing.T) {
	locked := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(locked, 0o555); err != nil {
		t.Fatal(err)
	}
	good := t.TempDir()

	dir, err := tempdir.Fallback(filepath.Join(locked, "нет-такого"), good)
	if err != nil {
		t.Fatalf("запасной каталог не нашёлся: %v", err)
	}
	if !strings.HasPrefix(dir, good) {
		t.Fatalf("выбран не тот каталог: %s", dir)
	}
	if err := tempdir.Writable(dir); err != nil {
		t.Fatalf("выбранный каталог недоступен для записи: %v", err)
	}
}

func TestSystemStaysSystemAfterEnsureMoves(t *testing.T) {
	before := tempdir.System()
	t.Setenv("TMPDIR", t.TempDir())
	if tempdir.System() != before {
		t.Fatal("подмена TMPDIR увела сокет управления за собой — клиент перестанет находить демона")
	}
}
