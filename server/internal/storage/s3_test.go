package storage_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"testing"

	"github.com/johannesboyne/gofakes3"
	"github.com/johannesboyne/gofakes3/backend/s3mem"

	"github.com/laminara/laminara/server/internal/storage"
)

func newS3Backend(t *testing.T) storage.Backend {
	t.Helper()
	backend, _ := newS3Fixture(t, nil)
	return backend
}

func newS3Fixture(t *testing.T, wrap func(http.Handler) http.Handler) (storage.Backend, *minio.Client) {
	t.Helper()
	memory := s3mem.New()
	handler := gofakes3.New(memory).Server()
	if wrap != nil {
		handler = wrap(handler)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
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
	client, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{
		Region: "us-east-1", Creds: credentials.NewStaticV4("test", "test", ""), BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	return backend, client
}

func TestS3Backend(t *testing.T) {
	backend := newS3Backend(t)
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
