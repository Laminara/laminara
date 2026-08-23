package catalog_test

import (
	"os"
	"path/filepath"
	"testing"

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
