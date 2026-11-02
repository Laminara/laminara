package buildsvc

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/buildspec"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/prepare"
	"github.com/laminara/laminara/server/internal/storage"
)

type syncTransport func(*http.Request) (*http.Response, error)

func (f syncTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func syncFixture(t *testing.T) (*Service, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	mux := http.NewServeMux()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { count.Add(1); mux.ServeHTTP(w, r) }))
	t.Cleanup(upstream.Close)
	mux.HandleFunc("/versions", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"versions":[{"id":"1.21.1","url":%q}]}`, upstream.URL+"/version")
	})
	mux.HandleFunc("/version", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"id":"1.21.1","mainClass":"net.minecraft.client.main.Main","javaVersion":{"component":"test","majorVersion":21},"assetIndex":{"id":"test","url":%q},"downloads":{"client":{"url":%q}}}`, upstream.URL+"/assets", upstream.URL+"/client")
	})
	mux.HandleFunc("/assets", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"objects":{}}`) })
	mux.HandleFunc("/client", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "client") })
	mux.HandleFunc("/runtimes", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"linux":{"test":[{"version":{"name":"21"},"manifest":{"url":%q}}]}}`, upstream.URL+"/runtime")
	})
	mux.HandleFunc("/runtime", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"files":{"bin/java":{"type":"file","executable":true,"downloads":{"raw":{"url":%q}}}}}`, upstream.URL+"/java")
	})
	mux.HandleFunc("/java", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "java") })
	mux.HandleFunc("/artifact/latest.json", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"version":"test","download_url":%q,"checksums":{"sha256":"%x"}}`, upstream.URL+"/authlib", sha256.Sum256([]byte("authlib")))
	})
	mux.HandleFunc("/authlib", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "authlib") })
	base, _ := url.Parse(upstream.URL)
	client := upstream.Client()
	transport := client.Transport
	client.Transport = syncTransport(func(request *http.Request) (*http.Response, error) {
		copy := request.Clone(request.Context())
		target := *request.URL
		target.Scheme = base.Scheme
		target.Host = base.Host
		copy.URL = &target
		return transport.RoundTrip(copy)
	})
	cfg, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3), manifest.NewSigner(key), t.TempDir())
	service.mojang = mojang.NewClientWith(client, upstream.URL+"/versions")
	service.preparer = prepare.NewPreparerWith(client, upstream.URL, upstream.URL+"/runtimes", 2)
	return service, &count
}

func TestBuildApplyUploadNoOpConflictFailureAndRollback(t *testing.T) {
	service, downloads := syncFixture(t)
	content := []byte("mod content")
	digest := fmt.Sprintf("%x", sha256.Sum256(content))
	spec := &buildspec.Spec{Version: 1, Name: "pack", Minecraft: "1.21.1", Loader: "vanilla", Compat: "off", Platforms: []string{"linux"}, RuntimeVersions: map[string]string{"linux": "21"}, SourceRevision: "first", Files: []buildspec.File{{Path: "mods/example.jar", SHA256: digest, Size: uint64(len(content))}}}
	data, _, _ := spec.Canonical()
	plan, err := service.PlanBuild(t.Context(), data)
	if err != nil || len(plan.MissingHashes) != 1 {
		t.Fatalf("plan: %v %v", plan, err)
	}
	chunk, err := service.UploadBuildChunk(t.Context(), &adminv1.UploadBuildChunkRequest{Sha256: digest, Size: uint64(len(content)), Data: content[:3]})
	if err != nil || chunk.Offset != 3 {
		t.Fatalf("chunk: %v %v", chunk, err)
	}
	chunk, err = service.UploadBuildChunk(t.Context(), &adminv1.UploadBuildChunkRequest{Sha256: digest, Size: uint64(len(content)), Data: content})
	if err != nil || chunk.Offset != 3 {
		t.Fatalf("resume: %v %v", chunk, err)
	}
	chunk, err = service.UploadBuildChunk(t.Context(), &adminv1.UploadBuildChunkRequest{Sha256: digest, Size: uint64(len(content)), Offset: 3, Data: content[3:]})
	if err != nil || !chunk.Complete {
		t.Fatalf("complete: %v %v", chunk, err)
	}
	if err := service.applyBuild(t.Context(), []string{string(data), plan.Revision}, io.Discard); err != nil {
		t.Fatal(err)
	}
	first, err := catalog.ReadRelease(service.profilesDir, "pack")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := os.ReadFile(filepath.Join(service.profileRoot("pack"), "mods", "example.jar"))
	if err != nil || !bytes.Equal(loaded, content) {
		t.Fatalf("workspace: %s %v", loaded, err)
	}
	plan, err = service.PlanBuild(t.Context(), data)
	if err != nil || !plan.NoOp {
		t.Fatalf("no-op: %v %v", plan, err)
	}
	before := downloads.Load()
	if err := service.applyBuild(t.Context(), []string{string(data), "old-retry"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	if downloads.Load() != before {
		t.Fatal("no-op downloaded again")
	}
	spec.RuntimeVersions["linux"] = "unexpected"
	wrongRuntime, _, _ := spec.Canonical()
	runtimePlan, err := service.PlanBuild(t.Context(), wrongRuntime)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.applyBuild(t.Context(), []string{string(wrongRuntime), runtimePlan.Revision}, io.Discard); err == nil {
		t.Fatal("different runtime version accepted")
	}
	unchanged, _ := catalog.ReadRelease(service.profilesDir, "pack")
	if unchanged.Current != first.Current {
		t.Fatal("runtime mismatch changed publication")
	}
	spec.RuntimeVersions["linux"] = "21"
	spec.SourceRevision = "second"
	data, _, _ = spec.Canonical()
	if err := service.applyBuild(t.Context(), []string{string(data), "stale"}, io.Discard); err == nil {
		t.Fatal("stale plan accepted")
	}
	plan, err = service.PlanBuild(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.applyBuild(t.Context(), []string{string(data), plan.Revision}, io.Discard); err != nil {
		t.Fatal(err)
	}
	second, err := catalog.ReadRelease(service.profilesDir, "pack")
	if err != nil || second.Current == first.Current {
		t.Fatalf("second release: %v %v", second, err)
	}
	spec.Platforms = append(spec.Platforms, "windows-x64")
	data, _, _ = spec.Canonical()
	plan, err = service.PlanBuild(t.Context(), data)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.applyBuild(t.Context(), []string{string(data), plan.Revision}, io.Discard); err == nil {
		t.Fatal("missing platform succeeded")
	}
	current, _ := catalog.ReadRelease(service.profilesDir, "pack")
	if current.Current != second.Current {
		t.Fatal("failure changed release")
	}
	if err := service.rollbackBuild(t.Context(), []string{"pack", first.Current}, io.Discard); err != nil {
		t.Fatal(err)
	}
	current, _ = catalog.ReadRelease(service.profilesDir, "pack")
	if current.Current != first.Current || current.SourceRevision != "first" {
		t.Fatal("rollback lost release metadata")
	}
	reader := catalog.New(service.profilesDir)
	if _, _, err := reader.Get("pack", corev1.Platform_PLATFORM_LINUX); err != nil {
		t.Fatal(err)
	}
	builds, err := service.Builds()
	if err != nil || len(builds) != 1 {
		t.Fatalf("hidden staging listed as build: %v %v", builds, err)
	}
}

func TestUploadRejectsTraversalAndCorruption(t *testing.T) {
	service := &Service{profilesDir: t.TempDir()}
	for _, hash := range []string{"../../outside", fmt.Sprintf("%x", sha256.Sum256([]byte("expected")))} {
		if _, err := service.UploadBuildChunk(t.Context(), &adminv1.UploadBuildChunkRequest{Sha256: hash, Size: 3, Data: []byte("bad")}); err == nil {
			t.Fatal("invalid upload accepted")
		}
	}
}
