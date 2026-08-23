package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/laminara/laminara/server/internal/storage"
)

func TestS3Backend(t *testing.T) {
	memory := s3mem.New()
	server := httptest.NewServer(gofakes3.New(memory).Server())
	defer server.Close()
	if err := memory.CreateBucket("laminara"); err != nil {
		t.Fatal(err)
	}

	config, _ := json.Marshal(map[string]any{
		"endpoint":        server.URL,
		"region":          "us-east-1",
		"bucket":          "laminara",
		"accessKeyId":     "test",
		"secretAccessKey": "test",
		"pathStyle":       true,
	})
	backend, err := storage.BuildBackend("s3", config)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	key := "objects/blake3/ab/cd/abcd"
	content := []byte("object storage content")

	if err := backend.Put(ctx, key, bytes.NewReader(content), int64(len(content))); err != nil {
		t.Fatalf("put: %v", err)
	}
	size, exists, err := backend.Stat(ctx, key)
	if err != nil || !exists || size != int64(len(content)) {
		t.Fatalf("stat: size=%d exists=%v err=%v", size, exists, err)
	}
	reader, err := backend.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(reader)
	reader.Close()
	if !bytes.Equal(got, content) {
		t.Fatalf("got %q", got)
	}
	location, err := backend.Locate(ctx, key, 0)
	if err != nil {
		t.Fatal(err)
	}
	if location.Kind != storage.LocationURL || location.URL == "" {
		t.Fatalf("locate: %+v", location)
	}
	if _, exists, _ := backend.Stat(ctx, "objects/nope"); exists {
		t.Fatal("missing object reported as existing")
	}
	if err := backend.Delete(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
}
