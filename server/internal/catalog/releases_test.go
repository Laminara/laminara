package catalog

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"google.golang.org/protobuf/proto"
)

func releaseFixture(t *testing.T, version string) []ReleaseFile {
	t.Helper()
	var files []ReleaseFile
	for _, p := range []corev1.Platform{corev1.Platform_PLATFORM_LINUX, corev1.Platform_PLATFORM_WINDOWS_X64} {
		data, err := manifest.Canonical(&corev1.Manifest{Modpack: "pack", Version: version, Platform: p})
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, ReleaseFile{Name: ManifestName("pack", p), Canonical: data, Signature: []byte(version)})
	}
	return files
}

func TestFailedReleaseCommitKeepsAllPreviousPlatforms(t *testing.T) {
	dir := t.TempDir()
	if err := PublishRelease(dir, "pack", releaseFixture(t, "old"), "revision-old"); err != nil {
		t.Fatal(err)
	}
	injected := errors.New("commit interrupted")
	err := publishRelease(dir, "pack", releaseFixture(t, "new"), "revision-new", func(string, []byte) error { return injected })
	if !errors.Is(err, injected) {
		t.Fatal(err)
	}
	reader := New(dir)
	for _, p := range []corev1.Platform{corev1.Platform_PLATFORM_LINUX, corev1.Platform_PLATFORM_WINDOWS_X64} {
		data, sig, err := reader.Get("pack", p)
		if err != nil {
			t.Fatal(err)
		}
		var m corev1.Manifest
		if err := proto.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Version != "old" || string(sig) != "old" {
			t.Fatalf("partial release: %s/%s", m.Version, sig)
		}
	}
	pointer, err := ReadRelease(dir, "pack")
	if err != nil || pointer.SettingsRevision != "revision-old" {
		t.Fatalf("pointer = %+v, %v", pointer, err)
	}
}

func TestConcurrentReleaseReturnsMatchingSignature(t *testing.T) {
	dir := t.TempDir()
	if err := PublishRelease(dir, "pack", releaseFixture(t, "0"), ""); err != nil {
		t.Fatal(err)
	}
	reader := New(dir)
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 8 {
			if err := PublishRelease(dir, "pack", releaseFixture(t, strconv.Itoa(i+1)), ""); err != nil {
				t.Error(err)
				return
			}
			reader.Refresh()
		}
	})
	for range 4 {
		workers.Go(func() {
			for range 40 {
				data, sig, err := reader.Get("pack", corev1.Platform_PLATFORM_LINUX)
				if err != nil {
					t.Error(err)
					return
				}
				var m corev1.Manifest
				if err := proto.Unmarshal(data, &m); err != nil {
					t.Error(err)
					return
				}
				if m.Version != string(sig) {
					t.Errorf("mixed pair: %s/%s", m.Version, sig)
					return
				}
			}
		})
	}
	workers.Wait()
}

func TestBrokenPointerDoesNotResurrectLegacyPublication(t *testing.T) {
	dir := t.TempDir()
	file := releaseFixture(t, "legacy")[0]
	if err := os.WriteFile(filepath.Join(dir, file.Name), file.Canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, SignaturePath(file.Name)), file.Signature, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := PublishRelease(dir, "pack", releaseFixture(t, "new"), ""); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(releaseRoot(dir, "pack"), "current.json"), []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	reader := New(dir)
	if err := reader.Check(); err == nil {
		t.Fatal("broken pointer reported healthy")
	}
	if _, _, err := reader.Get("pack", corev1.Platform_PLATFORM_LINUX); err == nil {
		t.Fatal("legacy release resurrected")
	}
}

func TestReleaseInventoryDetectsMissingPlatform(t *testing.T) {
	dir := t.TempDir()
	if err := PublishRelease(dir, "pack", releaseFixture(t, "first"), ""); err != nil {
		t.Fatal(err)
	}
	pointer, err := ReadRelease(dir, "pack")
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(releaseRoot(dir, "pack"), "releases", pointer.Current)
	name := ManifestName("pack", corev1.Platform_PLATFORM_WINDOWS_X64)
	if err := os.Remove(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, SignaturePath(name))); err != nil {
		t.Fatal(err)
	}
	if err := New(dir).Check(); err == nil {
		t.Fatal("publication with a missing platform reported healthy")
	}
}

func TestMigratedLegacyReleaseCanBeRolledBack(t *testing.T) {
	dir := t.TempDir()
	for _, file := range releaseFixture(t, "legacy") {
		if err := os.WriteFile(filepath.Join(dir, file.Name), file.Canonical, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, SignaturePath(file.Name)), file.Signature, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := PublishRelease(dir, "pack", releaseFixture(t, "new"), "new-settings"); err != nil {
		t.Fatal(err)
	}
	current, err := ReadRelease(dir, "pack")
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Retained) != 1 {
		t.Fatalf("history = %v", current.Retained)
	}
	if err := RollbackRelease(dir, "pack", current.Retained[0]); err != nil {
		t.Fatal(err)
	}
	reader := New(dir)
	for _, file := range releaseFixture(t, "legacy") {
		var expected corev1.Manifest
		if err := proto.Unmarshal(file.Canonical, &expected); err != nil {
			t.Fatal(err)
		}
		data, signature, err := reader.Get("pack", expected.Platform)
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != string(file.Canonical) || string(signature) != string(file.Signature) {
			t.Fatal("legacy publication was not restored")
		}
	}
	if err := RollbackRelease(dir, "pack", current.Current); err != nil {
		t.Fatal(err)
	}
}
