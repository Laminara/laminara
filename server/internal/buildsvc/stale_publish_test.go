package buildsvc_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/buildsvc"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func TestPublishRefusesPlatformsLeftFromAnEarlierInstall(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "old")
	current := "1.7.10 compat=lwjgl3ify:3.0.33"
	writeInstalledProfile(t, root, "linux", current)
	writeInstalledProfile(t, root, "windows-x64", "1.7.10 loader=forge loaderVersion=10.13.4.1614-1.7.10")
	writeInstalledProfile(t, root, "mac-os", "")
	writeBuildSettings(t, root, manifest.Settings{Install: current})
	service := serviceOver(t, dir)

	err := publishError(service, "old")
	if err == nil {
		t.Fatal("publish must refuse platforms built by an earlier install")
	}
	for _, want := range []string{"mac-os, windows-x64", "install old " + current + " platform=mac-os,windows-x64", filepath.Join(root, "platforms")} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must mention %q: %v", want, err)
		}
	}
	for _, p := range []corev1.Platform{corev1.Platform_PLATFORM_LINUX, corev1.Platform_PLATFORM_WINDOWS_X64, corev1.Platform_PLATFORM_MAC_OS} {
		if _, err := os.Stat(catalog.ManifestPath(dir, "old", p)); !os.IsNotExist(err) {
			t.Fatalf("a refused publish must not leave a manifest for %v: %v", p, err)
		}
	}

	writeInstalledProfile(t, root, "windows-x64", current)
	writeInstalledProfile(t, root, "mac-os", current)
	if err := publishError(service, "old"); err != nil {
		t.Fatalf("publish must go through once every platform comes from the last install: %v", err)
	}
}

func TestPublishRefusesABuildWhoseLastInstallFinishedNothing(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "gone")
	writeInstalledProfile(t, root, "linux", "1.7.10 compat=lwjgl3ify:3.0.33")
	writeInstalledProfile(t, root, "windows-x64", "1.7.10 compat=lwjgl3ify:3.0.33")
	writeBuildSettings(t, root, manifest.Settings{Install: "1.7.10 loader=forge loaderVersion=10.13.4.1614", Compat: "lwjgl3ify:3.0.33"})

	err := publishError(serviceOver(t, dir), "gone")
	if err == nil {
		t.Fatal("publish must refuse a build whose shared files the last install may have changed")
	}
	for _, want := range []string{"ни одной платформы", "install gone 1.7.10 loader=forge loaderVersion=10.13.4.1614 compat=нет platform=linux,windows-x64"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal must mention %q: %v", want, err)
		}
	}
	if strings.Contains(err.Error(), "остальной сборкой") {
		t.Errorf("with every platform behind there is no rest of the build to drift from: %v", err)
	}
}

func TestPublishTrustsBuildsWithoutARecordedInstall(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "legacy")
	writeInstalledProfile(t, root, "linux", "")
	writeInstalledProfile(t, root, "windows-x64", "")
	writeBuildSettings(t, root, manifest.Settings{})

	if err := publishError(serviceOver(t, dir), "legacy"); err != nil {
		t.Fatalf("a build made before installs were recorded must still publish: %v", err)
	}
}

func writeInstalledProfile(t *testing.T, root, platformKey, install string) {
	t.Helper()
	dir := filepath.Join(root, "platforms", platformKey)
	for _, path := range []string{filepath.Join(root, "mods"), dir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	launch, err := json.Marshal(manifest.LaunchProfile{VersionID: "1.7.10", PlatformKey: platformKey, Install: install})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.LaunchProfileName), launch, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "mods", "a.jar"), []byte("mod-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeBuildSettings(t *testing.T, root string, settings manifest.Settings) {
	t.Helper()
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest.SettingsFileName), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func serviceOver(t *testing.T, dir string) *buildsvc.Service {
	t.Helper()
	config, err := json.Marshal(map[string]string{"root": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return buildsvc.NewService(storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3), manifest.NewSigner(priv), dir)
}

func publishError(service *buildsvc.Service, name string) error {
	for _, cmd := range service.Commands() {
		if cmd.Name == "publish" {
			var out bytes.Buffer
			return cmd.Run(context.Background(), []string{name}, &out)
		}
	}
	return os.ErrNotExist
}
