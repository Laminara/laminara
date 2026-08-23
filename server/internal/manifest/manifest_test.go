package manifest_test

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
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func newCAS(t *testing.T) (*storage.CAS, storage.Backend) {
	t.Helper()
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	return storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3), backend
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hashRef(value byte) *corev1.ManifestFile {
	return &corev1.ManifestFile{
		Object: &corev1.ObjectRef{Hash: &corev1.Hash{Algo: corev1.HashAlgo_HASH_ALGO_BLAKE3, Value: []byte{value}}},
	}
}

func TestBuildSignVerify(t *testing.T) {
	ctx := context.Background()
	cas, _ := newCAS(t)
	root := writeTree(t, map[string]string{
		"mods/a.jar":   "alpha",
		"config/b.txt": "bravo",
	})
	built, err := manifest.NewBuilder(cas).Build(ctx, root, "test-pack", "1.0.0")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if len(built.Files) != 2 {
		t.Fatalf("files = %d", len(built.Files))
	}
	if built.TotalSize != uint64(len("alpha")+len("bravo")) {
		t.Fatalf("total = %d", built.TotalSize)
	}

	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	canonical, signature, err := manifest.NewSigner(priv).Sign(built)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Verify(pub, canonical, signature) {
		t.Fatal("valid signature rejected")
	}
	tampered := bytes.Clone(canonical)
	tampered[len(tampered)-1] ^= 0xff
	if manifest.Verify(pub, tampered, signature) {
		t.Fatal("tampered manifest verified")
	}
}

func TestCanonicalOrderIndependent(t *testing.T) {
	a := &corev1.Manifest{Files: []*corev1.ManifestFile{
		func() *corev1.ManifestFile { f := hashRef(2); f.Path = "b"; return f }(),
		func() *corev1.ManifestFile { f := hashRef(1); f.Path = "a"; return f }(),
	}}
	b := &corev1.Manifest{Files: []*corev1.ManifestFile{
		func() *corev1.ManifestFile { f := hashRef(1); f.Path = "a"; return f }(),
		func() *corev1.ManifestFile { f := hashRef(2); f.Path = "b"; return f }(),
	}}
	ca, err := manifest.Canonical(a)
	if err != nil {
		t.Fatal(err)
	}
	cb, err := manifest.Canonical(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ca, cb) {
		t.Fatal("canonical form depends on file order")
	}
}

func TestGCCollectsOrphans(t *testing.T) {
	ctx := context.Background()
	cas, backend := newCAS(t)
	root := writeTree(t, map[string]string{"mods/a.jar": "alpha"})
	built, err := manifest.NewBuilder(cas).Build(ctx, root, "p", "1")
	if err != nil {
		t.Fatal(err)
	}
	orphan, err := cas.Put(ctx, strings.NewReader("garbage"))
	if err != nil {
		t.Fatal(err)
	}

	deleted, err := manifest.GC(ctx, backend, []*corev1.Manifest{built})
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
	if has, _ := cas.Has(ctx, orphan); has {
		t.Fatal("orphan was not collected")
	}
	if has, _ := cas.Has(ctx, built.Files[0].Object); !has {
		t.Fatal("referenced object was collected")
	}
}
