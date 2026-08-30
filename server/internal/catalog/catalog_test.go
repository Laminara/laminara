package catalog_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/manifest"
)

func TestCatalog(t *testing.T) {
	dir := t.TempDir()
	canonical, err := manifest.Canonical(&corev1.Manifest{Modpack: "pack", Version: "3"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.manifest"), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.manifest.sig"), []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := catalog.New(dir)

	names, err := c.List()
	if err != nil || len(names) != 1 || names[0] != "pack" {
		t.Fatalf("list = %v err = %v", names, err)
	}
	gotCanonical, gotSig, err := c.Get("pack", corev1.Platform_PLATFORM_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCanonical) != string(canonical) || string(gotSig) != "sig" {
		t.Fatal("get returned unexpected bytes")
	}
	summaries, err := c.Summaries(corev1.Platform_PLATFORM_UNSPECIFIED)
	if err != nil || len(summaries) != 1 || summaries[0].Version != "3" {
		t.Fatalf("summaries = %+v err = %v", summaries, err)
	}
	if _, _, err := c.Get("missing", corev1.Platform_PLATFORM_UNSPECIFIED); err != catalog.ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func writeVariant(t *testing.T, dir, name string, p corev1.Platform, size uint64) {
	t.Helper()
	m := &corev1.Manifest{Modpack: name, Version: "1", Platform: p, TotalSize: size}
	canonical, err := manifest.Canonical(m)
	if err != nil {
		t.Fatal(err)
	}
	key := "unspecified"
	switch p {
	case corev1.Platform_PLATFORM_LINUX:
		key = "linux"
	case corev1.Platform_PLATFORM_WINDOWS_X64:
		key = "windows-x64"
	}
	base := filepath.Join(dir, name+"."+key)
	if err := os.WriteFile(base+".manifest", canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(base+".manifest.sig", []byte("sig-"+key), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCatalogRoutesPerPlatform(t *testing.T) {
	dir := t.TempDir()
	writeVariant(t, dir, "multi", corev1.Platform_PLATFORM_LINUX, 111)
	writeVariant(t, dir, "multi", corev1.Platform_PLATFORM_WINDOWS_X64, 222)
	c := catalog.New(dir)

	names, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "multi" {
		t.Fatalf("platform variants must collapse into one build: %v", names)
	}

	_, sig, err := c.Get("multi", corev1.Platform_PLATFORM_WINDOWS_X64)
	if err != nil {
		t.Fatal(err)
	}
	if string(sig) != "sig-windows-x64" {
		t.Fatalf("wrong variant served: %s", sig)
	}

	if _, _, err := c.Get("multi", corev1.Platform_PLATFORM_MAC_OS_ARM64); err != catalog.ErrPlatformUnavailable {
		t.Fatalf("unprepared platform must be reported, got %v", err)
	}

	summaries, err := c.Summaries(corev1.Platform_PLATFORM_LINUX)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 1 || summaries[0].TotalSize != 111 {
		t.Fatalf("summary must size the caller's platform: %+v", summaries)
	}
	if len(summaries[0].Platforms) != 2 {
		t.Fatalf("summary must advertise both platforms: %+v", summaries[0].Platforms)
	}
}

func TestCatalogSkipsBrokenManifest(t *testing.T) {
	dir := t.TempDir()
	canonical, err := manifest.Canonical(&corev1.Manifest{Modpack: "good", Version: "7"})
	if err != nil {
		t.Fatal(err)
	}
	broken := []byte("laminara: запись оборвалась на середине")
	if err := proto.Unmarshal(broken, &corev1.Manifest{}); err == nil {
		t.Fatal("test data must not parse as a manifest")
	}
	for name, data := range map[string][]byte{
		"good.manifest":     canonical,
		"broken.manifest":   broken,
		"good.manifest.sig": []byte("sig-good"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	c := catalog.New(dir)

	names, err := c.List()
	if err != nil || len(names) != 1 || names[0] != "good" {
		t.Fatalf("broken manifest must not hide the valid one: %v, err = %v", names, err)
	}
	canonicalGot, signature, err := c.Get("good", corev1.Platform_PLATFORM_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	if string(canonicalGot) != string(canonical) || string(signature) != "sig-good" {
		t.Fatal("get returned unexpected bytes")
	}
	summaries, err := c.Summaries(corev1.Platform_PLATFORM_UNSPECIFIED)
	if err != nil || len(summaries) != 1 || summaries[0].Version != "7" {
		t.Fatalf("summaries = %+v err = %v", summaries, err)
	}
	details, err := c.Details()
	if err != nil || len(details) != 1 {
		t.Fatalf("details = %+v err = %v", details, err)
	}

	fixed, err := manifest.Canonical(&corev1.Manifest{Modpack: "broken", Version: "9"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.manifest"), fixed, 0o644); err != nil {
		t.Fatal(err)
	}
	c.Refresh()
	names, err = c.List()
	if err != nil || len(names) != 2 {
		t.Fatalf("repaired manifest must be picked up: %v, err = %v", names, err)
	}
}

func TestCatalogServesSnapshotWithoutRescan(t *testing.T) {
	dir := t.TempDir()
	canonical, err := manifest.Canonical(&corev1.Manifest{Modpack: "pack"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.ManifestPath(dir, "pack", corev1.Platform_PLATFORM_UNSPECIFIED), canonical, 0o644); err != nil {
		t.Fatal(err)
	}

	c := catalog.New(dir)
	if _, err := c.List(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(catalog.ManifestPath(dir, "pack", corev1.Platform_PLATFORM_UNSPECIFIED)); err != nil {
		t.Fatal(err)
	}
	if names, err := c.List(); err != nil || len(names) != 1 {
		t.Fatalf("hot path must serve the ready snapshot: %v, err = %v", names, err)
	}
	c.Refresh()
	names, err := c.List()
	if err != nil || len(names) != 0 {
		t.Fatalf("refresh must notice the manifest is gone: %v, err = %v", names, err)
	}
}

func TestCatalogKeepsSnapshotWhileFolderIsAway(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "builds")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, err := manifest.Canonical(&corev1.Manifest{Modpack: "pack", Version: "4"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.manifest"), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.manifest.sig"), []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := catalog.New(dir)
	if names, err := c.List(); err != nil || len(names) != 1 {
		t.Fatalf("list = %v, err = %v", names, err)
	}

	away := dir + ".away"
	if err := os.Rename(dir, away); err != nil {
		t.Fatal(err)
	}
	back := false
	t.Cleanup(func() {
		if back {
			return
		}
		if err := os.Rename(away, dir); err != nil {
			t.Fatal(err)
		}
	})

	c.Refresh()
	names, err := c.List()
	if err != nil || len(names) != 1 || names[0] != "pack" {
		t.Fatalf("a folder that went away must not wipe the build: %v, err = %v", names, err)
	}
	if summaries, err := c.Summaries(corev1.Platform_PLATFORM_UNSPECIFIED); err != nil || len(summaries) != 1 || summaries[0].Version != "4" {
		t.Fatalf("summaries = %+v, err = %v", summaries, err)
	}

	if err := os.Rename(away, dir); err != nil {
		t.Fatal(err)
	}
	back = true
	c.Refresh()
	if names, err := c.List(); err != nil || len(names) != 1 {
		t.Fatalf("the build must come back: %v, err = %v", names, err)
	}
}

func TestCatalogConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	canonical, err := manifest.Canonical(&corev1.Manifest{Modpack: "pack", Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.ManifestPath(dir, "pack", corev1.Platform_PLATFORM_UNSPECIFIED), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalog.SignaturePath(catalog.ManifestPath(dir, "pack", corev1.Platform_PLATFORM_UNSPECIFIED)), []byte("sig"), 0o644); err != nil {
		t.Fatal(err)
	}

	c := catalog.New(dir)
	var wg sync.WaitGroup
	failure := make(chan error, 1)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 25; j++ {
				names, err := c.List()
				if err != nil {
					failure <- err
					return
				}
				if len(names) != 1 {
					failure <- errors.New("unexpected list " + strings.Join(names, ", "))
					return
				}
				if _, _, err := c.Get("pack", corev1.Platform_PLATFORM_UNSPECIFIED); err != nil {
					failure <- err
					return
				}
				if _, err := c.Summaries(corev1.Platform_PLATFORM_UNSPECIFIED); err != nil {
					failure <- err
					return
				}
				c.Refresh()
			}
		}()
	}
	wg.Wait()
	select {
	case err := <-failure:
		t.Fatal(err)
	default:
	}
}

func TestManifestNames(t *testing.T) {
	if got := catalog.ManifestName("pack", corev1.Platform_PLATFORM_UNSPECIFIED); got != "pack.manifest" {
		t.Fatalf("flat build is published under its own name: %s", got)
	}
	if got := catalog.ManifestName("multi", corev1.Platform_PLATFORM_LINUX); got != "multi.linux.manifest" {
		t.Fatalf("platform variant must carry the platform key: %s", got)
	}
	if got := catalog.SignaturePath("pack.linux.manifest"); got != "pack.linux.manifest.sig" {
		t.Fatalf("signature sits next to the manifest: %s", got)
	}
	if got := catalog.SignaturePath(filepath.Join(t.TempDir(), "pack.manifest")); !strings.HasSuffix(got, "pack.manifest.sig") {
		t.Fatalf("signature path must follow the manifest path: %s", got)
	}
}
