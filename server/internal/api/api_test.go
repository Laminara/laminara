package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"connectrpc.com/connect"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/api"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func TestListAndGetManifest(t *testing.T) {
	dir := t.TempDir()
	canonical, _ := manifest.Canonical(&corev1.Manifest{Modpack: "pack", Version: "1"})
	if err := os.WriteFile(filepath.Join(dir, "pack.manifest"), canonical, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "pack.manifest.sig"), []byte{1, 2, 3}, 0o644); err != nil {
		t.Fatal(err)
	}

	svc := api.NewService(api.Options{Catalog: catalog.New(dir)})
	ctx := context.Background()

	list, err := svc.ListProfiles(ctx, connect.NewRequest(&apiv1.ListProfilesRequest{}))
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Msg.Profiles) != 1 || list.Msg.Profiles[0].Name != "pack" || list.Msg.Profiles[0].Version != "1" {
		t.Fatalf("profiles = %+v", list.Msg.Profiles)
	}

	got, err := svc.GetManifest(ctx, connect.NewRequest(&apiv1.GetManifestRequest{Profile: "pack"}))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Msg.Manifest, canonical) || !bytes.Equal(got.Msg.Signature, []byte{1, 2, 3}) {
		t.Fatal("manifest bytes mismatch")
	}

	if _, err := svc.GetManifest(ctx, connect.NewRequest(&apiv1.GetManifestRequest{Profile: "missing"})); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatalf("expected NotFound, got %v", err)
	}
	if _, err := svc.Login(ctx, connect.NewRequest(&apiv1.LoginRequest{})); connect.CodeOf(err) != connect.CodeUnimplemented {
		t.Fatalf("expected Unimplemented for nil auth, got %v", err)
	}
}

func TestObjectHandler(t *testing.T) {
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "objects/blake3/ab/cd/abcd", bytes.NewReader([]byte("HELLO")), 5); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.ObjectHandler(backend, false))
	defer server.Close()

	resp, err := http.Get(server.URL + "/objects/objects/blake3/ab/cd/abcd")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != "HELLO" {
		t.Fatalf("body = %q", body)
	}

	missing, err := http.Get(server.URL + "/objects/nope")
	if err != nil {
		t.Fatal(err)
	}
	missing.Body.Close()
	if missing.StatusCode != http.StatusNotFound {
		t.Fatalf("missing status = %d", missing.StatusCode)
	}
}

func TestObjectHandlerXAccel(t *testing.T) {
	config, _ := json.Marshal(map[string]string{"root": t.TempDir(), "xaccelPrefix": "/internal-objects/"})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	if err := backend.Put(context.Background(), "objects/blake3/ab/cd/abcd", bytes.NewReader([]byte("HELLO")), 5); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.ObjectHandler(backend, true))
	defer server.Close()

	resp, err := http.Get(server.URL + "/objects/objects/blake3/ab/cd/abcd")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if got := resp.Header.Get("X-Accel-Redirect"); got != "/internal-objects/objects/blake3/ab/cd/abcd" {
		t.Fatalf("X-Accel-Redirect = %q", got)
	}
	if len(body) != 0 {
		t.Fatalf("expected empty body under x-accel, got %d bytes", len(body))
	}
}
