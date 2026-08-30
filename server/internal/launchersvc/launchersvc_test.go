package launchersvc

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func TestClassifyRecognisesWhatTheBuildScriptProduces(t *testing.T) {
	cases := map[string]struct {
		platform corev1.Platform
		kind     corev1.LauncherArtifactKind
	}{
		"Laminara":                   {corev1.Platform_PLATFORM_LINUX, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE},
		"Laminara.exe":               {corev1.Platform_PLATFORM_WINDOWS_X64, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE},
		"Laminara-linux-arm64":       {corev1.Platform_PLATFORM_LINUX_ARM64, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE},
		"Laminara-setup.exe":         {corev1.Platform_PLATFORM_WINDOWS_X64, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER},
		"Laminara.AppImage":          {corev1.Platform_PLATFORM_LINUX, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_IMAGE},
		"Laminara-mac-os.app.tar.gz": {corev1.Platform_PLATFORM_MAC_OS, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_BUNDLE_TAR_GZ},
	}
	for name, want := range cases {
		platform, kind, ok := classify(name)
		if !ok {
			t.Fatalf("%s was not recognised", name)
		}
		if platform != want.platform || kind != want.kind {
			t.Fatalf("%s = %v/%v, want %v/%v", name, platform, kind, want.platform, want.kind)
		}
	}
}

func TestClassifyRejectsWhatIsNotALauncher(t *testing.T) {
	for _, name := range []string{"notes.txt", "readme.md", "release.json"} {
		if _, _, ok := classify(name); ok {
			t.Fatalf("%s must not be taken for a launcher", name)
		}
	}
}

func TestPublishOrdersVersionsIncludingPrereleases(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3), manifest.NewSigner(priv), dir)

	put := func(number string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, number), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, number, "Laminara"), []byte("launcher"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	publish := func(number string) error {
		t.Helper()
		put(number)
		return service.publish(ctx, number, &bytes.Buffer{})
	}

	if err := publish("1.2.3-beta"); err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if err := publish("1.2.3"); err != nil {
		t.Fatalf("a release must outrank its own prerelease: %v", err)
	}
	if err := publish("1.2.3-rc2"); err == nil {
		t.Fatal("a prerelease must not replace the release that is already published")
	}
	if err := publish("1.2.3"); err == nil {
		t.Fatal("republishing the same version must be refused")
	}

	release, _, err := NewReleases(dir).Current()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(release)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != "1.2.3" {
		t.Fatalf("published version = %s, want 1.2.3", decoded.Version)
	}
}

func TestPublishRefusesWhatIsNotANumber(t *testing.T) {
	for _, number := range []string{"latest", "1.2.x", "", "первая", "1.2", "1", "1.2.3-"} {
		service := NewService(nil, nil, t.TempDir())
		if err := service.publish(context.Background(), number, &bytes.Buffer{}); err == nil {
			t.Errorf("version %q was accepted", number)
		}
	}
}
