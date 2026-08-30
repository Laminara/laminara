package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/storage"
)

func newCAS(t *testing.T) *storage.CAS {
	t.Helper()
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	return storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
}

func TestCASPutGet(t *testing.T) {
	ctx := context.Background()
	cas := newCAS(t)

	ref, err := cas.Put(ctx, strings.NewReader("hello world"))
	if err != nil {
		t.Fatalf("put: %v", err)
	}
	if ref.Size != 11 {
		t.Fatalf("size = %d", ref.Size)
	}

	reader, err := cas.Get(ctx, ref)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer reader.Close()
	data, _ := io.ReadAll(reader)
	if string(data) != "hello world" {
		t.Fatalf("got %q", data)
	}
}

func TestCASDeduplicates(t *testing.T) {
	ctx := context.Background()
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)

	first, err := cas.Put(ctx, strings.NewReader("same content"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := cas.Put(ctx, strings.NewReader("same content"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Hash.Value, second.Hash.Value) {
		t.Fatal("identical content produced different hashes")
	}
	key := storage.ObjectKey(first.Hash.Algo, first.Hash.Value)
	if _, exists, err := backend.Stat(ctx, key); err != nil || !exists {
		t.Fatalf("expected %s to be stored once, exists=%v err=%v", key, exists, err)
	}
}

func TestObjectKeyLayout(t *testing.T) {
	sum := []byte{0xab, 0xcd, 0xef}
	key := storage.ObjectKey(corev1.HashAlgo_HASH_ALGO_BLAKE3, sum)
	if key != "blake3/ab/cd/abcdef" {
		t.Fatalf("got %q", key)
	}
}

func TestFSLocate(t *testing.T) {
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	loc, err := backend.Locate(context.Background(), "objects/blake3/ab/cd/abcd", 0)
	if err != nil {
		t.Fatal(err)
	}
	if loc.Kind != storage.LocationInternal || loc.InternalPath != "/_objects/objects/blake3/ab/cd/abcd" {
		t.Fatalf("got %+v", loc)
	}
}

func TestPutFileStoresContentAndSkipsWorkTheSecondTime(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "objects")
	backend, err := storage.BuildBackend("fs", json.RawMessage(`{"root":"`+filepath.ToSlash(root)+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
	ctx := context.Background()

	payload := bytes.Repeat([]byte("laminara"), 4096)
	source := filepath.Join(dir, "client.jar")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}

	ref, err := cas.PutFile(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if ref.Size != uint64(len(payload)) {
		t.Fatalf("size = %d, want %d", ref.Size, len(payload))
	}

	stored, err := cas.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stored)
	stored.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("stored bytes differ from the source")
	}

	viaReader, err := cas.Put(ctx, bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(viaReader.Hash.Value, ref.Hash.Value) {
		t.Fatal("a file and a stream of the same bytes produced different objects")
	}

	key := storage.ObjectKey(ref.Hash.Algo, ref.Hash.Value)
	before, err := os.Stat(filepath.Join(root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatal(err)
	}

	again, err := cas.PutFile(ctx, source)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(again.Hash.Value, ref.Hash.Value) {
		t.Fatal("the same file hashed differently")
	}
	after, err := os.Stat(filepath.Join(root, filepath.FromSlash(key)))
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("an object that was already stored got rewritten")
	}

	if err := os.WriteFile(source, []byte("подменили"), 0o644); err != nil {
		t.Fatal(err)
	}
	stored, err = cas.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err = io.ReadAll(stored)
	stored.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("the published object followed a later edit of the source")
	}
}

type racingBackend struct {
	storage.Backend
	source  string
	replace []byte
}

func (b *racingBackend) PutFromFile(ctx context.Context, key, src string, verify func(io.Reader) error) error {
	if err := os.WriteFile(b.source, b.replace, 0o644); err != nil {
		return err
	}
	return b.Backend.(storage.FileSource).PutFromFile(ctx, key, src, verify)
}

type plainBackend struct {
	storage.Backend
	source  string
	replace []byte
}

func (b *plainBackend) Put(ctx context.Context, key string, r io.Reader, size int64) error {
	if b.source != "" {
		if err := os.WriteFile(b.source, b.replace, 0o644); err != nil {
			return err
		}
	}
	return b.Backend.Put(ctx, key, r, size)
}

func newBackend(t *testing.T) storage.Backend {
	t.Helper()
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	return backend
}

func writeSource(t *testing.T, payload []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client.jar")
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func refOf(t *testing.T, payload []byte) *corev1.ObjectRef {
	t.Helper()
	ref, err := storage.NewCAS(newBackend(t), corev1.HashAlgo_HASH_ALGO_BLAKE3).Put(context.Background(), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestPutFileRefusesAFileThatChangedWhileItWasPublished(t *testing.T) {
	ctx := context.Background()
	payload := []byte("the bytes that were hashed")
	source := writeSource(t, payload)
	inner := newBackend(t)
	backend := &racingBackend{Backend: inner, source: source, replace: []byte("something else entirely")}

	if _, err := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3).PutFile(ctx, source); err == nil {
		t.Fatal("a file that changed under us must not be published")
	}

	key := storage.ObjectKey(corev1.HashAlgo_HASH_ALGO_BLAKE3, refOf(t, payload).Hash.Value)
	if _, exists, err := inner.Stat(ctx, key); err != nil || exists {
		t.Fatal("nothing must be left under the key of the original bytes")
	}
}

func TestStreamedPutFileRefusesAFileThatChangedWhileItWasPublished(t *testing.T) {
	ctx := context.Background()
	payload := bytes.Repeat([]byte("s3"), 1024)
	source := writeSource(t, payload)
	inner := newBackend(t)
	backend := &plainBackend{Backend: inner, source: source, replace: []byte("something else entirely")}

	if _, err := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3).PutFile(ctx, source); err == nil {
		t.Fatal("a file that changed under us must not be published")
	}

	key := storage.ObjectKey(corev1.HashAlgo_HASH_ALGO_BLAKE3, refOf(t, payload).Hash.Value)
	if _, exists, err := inner.Stat(ctx, key); err != nil || exists {
		t.Fatal("the mismatching object must be removed from the store")
	}
}

func TestPutFromFileLeavesNothingBehindWhenVerificationFails(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	config, _ := json.Marshal(map[string]string{"root": root})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	source := writeSource(t, []byte("a jar"))

	refused := errors.New("refused")
	err = backend.(storage.FileSource).PutFromFile(ctx, "blake3/aa/bb/aabb", source, func(io.Reader) error { return refused })
	if !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the verification error", err)
	}
	if _, exists, err := backend.Stat(ctx, "blake3/aa/bb/aabb"); err != nil || exists {
		t.Fatal("a refused object must never appear under its key")
	}
	entries, err := os.ReadDir(filepath.Join(root, "blake3", "aa", "bb"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestPutFileFallsBackToStreamingForBackendsWithoutFileSupport(t *testing.T) {
	ctx := context.Background()
	cas := storage.NewCAS(&plainBackend{Backend: newBackend(t)}, corev1.HashAlgo_HASH_ALGO_BLAKE3)

	payload := bytes.Repeat([]byte("s3"), 1024)
	ref, err := cas.PutFile(ctx, writeSource(t, payload))
	if err != nil {
		t.Fatal(err)
	}
	stored, err := cas.Get(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(stored)
	stored.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("streamed object differs from the source")
	}
}

func TestPutFileReportsAMissingSource(t *testing.T) {
	cas := storage.NewCAS(newBackend(t), corev1.HashAlgo_HASH_ALGO_BLAKE3)
	if _, err := cas.PutFile(context.Background(), filepath.Join(t.TempDir(), "absent.jar")); err == nil {
		t.Fatal("a missing source must be reported")
	}
}
