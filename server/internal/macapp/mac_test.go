package macapp

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func codesignVerify(t *testing.T, path string) (string, bool) {
	t.Helper()
	output, err := exec.Command("codesign", "--verify", "--verbose", path).CombinedOutput()
	return string(output), err == nil
}

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
	if output, ok := codesignVerify(t, binary); !ok {
		t.Fatalf("исходный файл не подписан, дальше проверять нечего:\n%s", output)
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
	if output, err := exec.Command("plutil", "-lint", filepath.Join(app, "Contents/Info.plist")).CombinedOutput(); err != nil {
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

	unpacked, err := os.ReadFile(inside)
	if err != nil {
		t.Fatal(err)
	}
	loose := filepath.Join(root, "laminara-loose")
	if err := os.WriteFile(loose, unpacked, 0o755); err != nil {
		t.Fatal(err)
	}
	if output, ok := codesignVerify(t, loose); !ok {
		t.Fatalf("после запаковки подпись сломалась — такой лаунчер Apple Silicon не запустит:\n%s", output)
	}

	whole, _ := codesignVerify(t, app)
	t.Logf("подпись пакета целиком (её у нас нет и быть не может, запечатать Resources может только мак):\n%s", whole)
	assessed, _ := exec.Command("spctl", "--assess", "--type", "execute", "--verbose", app).CombinedOutput()
	t.Logf("что о пакете думает Gatekeeper:\n%s", assessed)

	survives(t, inside)
}

func survives(t *testing.T, path string) {
	t.Helper()
	command := exec.Command(path)
	if err := command.Start(); err != nil {
		t.Fatalf("лаунчер из пакета не запускается: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		if command.ProcessState != nil && command.ProcessState.ExitCode() == -1 {
			t.Fatalf("система убила лаунчер сигналом — так выглядит отвергнутая подпись: %v", err)
		}
		t.Logf("лаунчер завершился сам (%v) — для проверки подписи это неважно", err)
	case <-time.After(8 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
	}
}
