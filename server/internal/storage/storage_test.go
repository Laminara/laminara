package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
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
	cas := newCAS(t)

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
	has, err := cas.Has(ctx, first)
	if err != nil || !has {
		t.Fatalf("expected object to exist, has=%v err=%v", has, err)
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
