package buildsvc

import (
	"os"
	"path/filepath"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/platform"
)

func put(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestSharingKeepsEveryFileAndDropsCopies(t *testing.T) {
	root := t.TempDir()
	for _, key := range []string{"linux", "windows-x64"} {
		put(t, filepath.Join(root, key, "laminara.profile.json"), `{"platformKey":"`+key+`"}`)
		put(t, filepath.Join(root, key, "assets", "sound.ogg"), "одинаковый ресурс")
		put(t, filepath.Join(root, key, "mods", "shared.jar"), "общий мод")
		put(t, filepath.Join(root, key, "runtime", "bin", "java"), "рантайм "+key)
	}
	put(t, filepath.Join(root, "windows-x64", "mods", "only-windows.jar"), "мод только для windows")

	linux, _ := platform.Parse("linux")
	windows, _ := platform.Parse("windows-x64")
	freed, err := shareCommonFiles(root, []corev1.Platform{linux, windows})
	if err != nil {
		t.Fatal(err)
	}

	if !exists(filepath.Join(root, "assets", "sound.ogg")) {
		t.Fatal("общий ресурс не переехал в корень сборки")
	}
	if read(t, filepath.Join(root, "mods", "shared.jar")) != "общий мод" {
		t.Fatal("общий мод не переехал в корень сборки")
	}
	if !exists(filepath.Join(root, "platforms", "linux", "runtime", "bin", "java")) {
		t.Fatal("рантайм linux не переехал в платформенную папку")
	}
	if read(t, filepath.Join(root, "platforms", "windows-x64", "runtime", "bin", "java")) != "рантайм windows-x64" {
		t.Fatal("рантайм windows затёрся рантаймом другой платформы")
	}
	if !exists(filepath.Join(root, "platforms", "windows-x64", "mods", "only-windows.jar")) {
		t.Fatal("файл, которого не было у другой платформы, потерян")
	}
	if exists(filepath.Join(root, "linux")) || exists(filepath.Join(root, "windows-x64")) {
		t.Fatal("старые папки платформ остались на диске")
	}
	if freed == 0 {
		t.Fatal("перекладывание не освободило ни байта, хотя копии были")
	}
}

func TestSharingIsRecognisedAsLayout(t *testing.T) {
	root := t.TempDir()
	service := &Service{profilesDir: root}
	build := filepath.Join(root, "pack")
	put(t, filepath.Join(build, "platforms", "linux", "laminara.profile.json"), `{"platformKey":"linux"}`)

	layout := service.layout("pack")
	if !layout.shared {
		t.Fatal("общая раскладка не распознана")
	}
	if len(layout.platforms) != 1 {
		t.Fatalf("платформы не найдены: %+v", layout.platforms)
	}
	placed := layout.place(layout.platforms[0])
	if placed.shared != build {
		t.Fatalf("общая папка должна быть корнем сборки, получили %s", placed.shared)
	}
	if placed.platform != filepath.Join(build, "platforms", "linux") {
		t.Fatalf("платформенная папка не там: %s", placed.platform)
	}
}

func TestOldLayoutStillRecognised(t *testing.T) {
	root := t.TempDir()
	service := &Service{profilesDir: root}
	put(t, filepath.Join(root, "pack", "linux", "laminara.profile.json"), `{"platformKey":"linux"}`)

	layout := service.layout("pack")
	if layout.shared {
		t.Fatal("старая раскладка принята за общую")
	}
	if len(layout.platforms) != 1 {
		t.Fatalf("платформа не найдена: %+v", layout.platforms)
	}
}
