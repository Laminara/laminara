package macapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBundleOnARealMacKeepsItsSignature(t *testing.T) {
	binary := os.Getenv("LAMINARA_MACAPP_BINARY")
	if binary == "" {
		t.Skip("нет LAMINARA_MACAPP_BINARY — тесту нужен собранный лаунчер для macOS")
	}
	if runtime.GOOS != "darwin" {
		t.Skip("проверка подписи идёт только на macOS")
	}
	executable, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	before := exec.Command("codesign", "--verify", "--verbose", binary)
	if output, err := before.CombinedOutput(); err != nil {
		t.Fatalf("исходный файл не подписан, дальше проверять нечего: %v\n%s", err, output)
	}

	bundle := Bundle{
		Name:       "Пример",
		Version:    "1.0.0",
		Executable: executable,
		Config:     []byte(`{"endpoints":[{"id":"play","baseUrl":"https://play.example"}]}`),
	}
	archive, err := bundle.TarGz()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	packed := filepath.Join(root, "launcher.app.tar.gz")
	if err := os.WriteFile(packed, archive, 0o644); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command("tar", "xzf", packed, "-C", root).CombinedOutput(); err != nil {
		t.Fatalf("пакет не распаковывается: %v\n%s", err, output)
	}

	app := filepath.Join(root, BundleName(bundle.Name))
	plist := exec.Command("plutil", "-lint", filepath.Join(app, "Contents/Info.plist"))
	if output, err := plist.CombinedOutput(); err != nil {
		t.Fatalf("Info.plist не проходит проверку: %v\n%s", err, output)
	}
	inside := filepath.Join(app, "Contents/MacOS", ExecutableName)
	info, err := os.Stat(inside)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("распакованный файл не исполняемый: %v", info.Mode())
	}
	after := exec.Command("codesign", "--verify", "--verbose", inside)
	if output, err := after.CombinedOutput(); err != nil {
		t.Fatalf("после запаковки подпись сломалась — такой лаунчер Apple Silicon не запустит: %v\n%s", err, output)
	}
}
