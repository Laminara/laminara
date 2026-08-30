package buildsvc_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/buildsvc"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func TestPublishIsServedByCatalog(t *testing.T) {
	dir := t.TempDir()
	writeProfile(t, filepath.Join(dir, "pack"), "")
	writeProfile(t, filepath.Join(dir, "boxed", "linux"), "linux")

	config, err := json.Marshal(map[string]string{"root": t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	service := buildsvc.NewService(storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3), manifest.NewSigner(priv), dir)
	published := catalog.New(dir)
	service.SetCatalog(published)

	run(t, service, "publish", "pack")
	run(t, service, "publish", "boxed")

	flat := catalog.ManifestPath(dir, "pack", corev1.Platform_PLATFORM_UNSPECIFIED)
	perPlatform := catalog.ManifestPath(dir, "boxed", corev1.Platform_PLATFORM_LINUX)
	for _, path := range []string{flat, catalog.SignaturePath(flat), perPlatform, catalog.SignaturePath(perPlatform)} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("publish must leave %s behind: %v", path, err)
		}
	}

	names, err := published.List()
	if err != nil || len(names) != 2 {
		t.Fatalf("catalog must see both published builds: %v, err = %v", names, err)
	}

	canonical, signature, err := published.Get("pack", corev1.Platform_PLATFORM_UNSPECIFIED)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(flat)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, stored) || len(signature) == 0 {
		t.Fatal("get must return the published manifest with its signature")
	}
	if !manifest.Verify(pub, canonical, signature) {
		t.Fatal("served manifest must verify against the publishing key")
	}
	var m corev1.Manifest
	if err := proto.Unmarshal(canonical, &m); err != nil {
		t.Fatal(err)
	}
	if m.Modpack != "pack" {
		t.Fatalf("catalog must keep the build name: %s", m.Modpack)
	}

	variant, variantSignature, err := published.Get("boxed", corev1.Platform_PLATFORM_LINUX)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Verify(pub, variant, variantSignature) {
		t.Fatal("served platform variant must verify against the publishing key")
	}
	if _, _, err := published.Get("boxed", corev1.Platform_PLATFORM_UNSPECIFIED); !errors.Is(err, catalog.ErrPlatformUnavailable) {
		t.Fatalf("unpublished platform must be reported, got %v", err)
	}

	run(t, service, "delete", "pack")
	for _, path := range []string{flat, catalog.SignaturePath(flat)} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("delete must remove %s: %v", path, err)
		}
	}
	published.Refresh()
	if names, err := published.List(); err != nil || len(names) != 1 || names[0] != "boxed" {
		t.Fatalf("deleted build must leave the catalog: %v, err = %v", names, err)
	}
}

func writeProfile(t *testing.T, dir, platformKey string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	launch, err := json.Marshal(manifest.LaunchProfile{VersionID: "1.21.1", PlatformKey: platformKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.LaunchProfileName), launch, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mods", "a.jar"), []byte("mod-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func run(t *testing.T, service *buildsvc.Service, name string, args ...string) {
	t.Helper()
	for _, cmd := range service.Commands() {
		if cmd.Name != name {
			continue
		}
		var out bytes.Buffer
		if err := cmd.Run(context.Background(), args, &out); err != nil {
			t.Fatalf("%s %s: %v", name, strings.Join(args, " "), err)
		}
		return
	}
	t.Fatalf("команда %s не зарегистрирована", name)
}
