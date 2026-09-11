package prepare

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/manifest"
)

func writeProfile(t *testing.T, dir, component string) {
	t.Helper()
	data, err := json.Marshal(manifest.LaunchProfile{JavaComponent: component, PlatformKey: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.LaunchProfileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestTheSameJavaKeepsItsRuntimeInPlace(t *testing.T) {
	platformDir := t.TempDir()
	writeProfile(t, platformDir, "jre-legacy")
	if movedToAnotherJava(platformDir, "jre-legacy") {
		t.Fatal("та же Java — рантайм трогать не надо")
	}
	if movedToAnotherJava(platformDir, "java-runtime-epsilon") != true {
		t.Fatal("смена Java должна быть замечена")
	}
}

func TestAFreshBuildHasNoPreviousJava(t *testing.T) {
	if movedToAnotherJava(t.TempDir(), "java-runtime-epsilon") {
		t.Fatal("у новой сборки прошлой Java нет")
	}
}

func TestTheOldRuntimeSurvivesUntilTheNewOneIsDownloaded(t *testing.T) {
	root := t.TempDir()
	writeProfile(t, root, "jre-legacy")

	live := filepath.Join(root, "runtime", "linux")
	if err := os.MkdirAll(live, 0o755); err != nil {
		t.Fatal(err)
	}
	working := filepath.Join(live, "bin")
	if err := os.WriteFile(working, []byte("старая java"), 0o755); err != nil {
		t.Fatal(err)
	}

	fresh := filepath.Join(root, "runtime", ".linux.new")
	if err := os.MkdirAll(fresh, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(working); err != nil {
		t.Fatal("пока новая Java качается, старая обязана остаться на месте — иначе оборванный install оставит сборку без Java")
	}
	if err := os.WriteFile(filepath.Join(fresh, "bin"), []byte("новая java"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := swapRuntime(root, "runtime/.linux.new", "runtime/linux", "java-runtime-epsilon"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(working)
	if err != nil || string(got) != "новая java" {
		t.Fatalf("после подмены в рантайме %q, %v", got, err)
	}
	if _, err := os.Stat(fresh); !os.IsNotExist(err) {
		t.Fatal("временная папка должна исчезнуть")
	}
}
