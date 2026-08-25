package prepare_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/prepare"
	"github.com/laminara/laminara/server/internal/storage"
)

func TestPrepareAndPublish(t *testing.T) {
	assetContent := "asset-bytes"
	sum := sha1.Sum([]byte(assetContent))
	assetHash := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/version.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"id": "1.21.1",
			"type": "release",
			"mainClass": "net.minecraft.client.main.Main",
			"javaVersion": { "component": "java-runtime-delta", "majorVersion": 21 },
			"assetIndex": { "id": "17", "url": "%s/assets.json" },
			"libraries": [
				{ "name": "com.example:x:1", "downloads": { "artifact": { "path": "com/example/x/1/x-1.jar", "url": "%s/lib.jar" } } }
			],
			"downloads": { "client": { "url": "%s/client.jar" } }
		}`, server.URL, server.URL, server.URL)
	})
	mux.HandleFunc("/client.jar", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "CLIENTJAR") })
	mux.HandleFunc("/lib.jar", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "LIB") })
	mux.HandleFunc("/assets.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{ "objects": { "minecraft/lang/en_us.json": { "hash": "%s", "size": %d } } }`, assetHash, len(assetContent))
	})
	mux.HandleFunc("/"+assetHash[:2]+"/"+assetHash, func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, assetContent) })
	mux.HandleFunc("/jre/all.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{ "linux": { "java-runtime-delta": [ { "version": { "name": "21.0.3" }, "manifest": { "url": "%s/jre/runtime.json" } } ] } }`, server.URL)
	})
	mux.HandleFunc("/jre/runtime.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{ "files": { "bin/java": { "type": "file", "executable": true, "downloads": { "raw": { "url": "%s/jre/java" } } } } }`, server.URL)
	})
	mux.HandleFunc("/jre/java", func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "JAVA") })

	profileDir := t.TempDir()
	preparer := prepare.NewPreparerWith(server.Client(), server.URL, server.URL+"/jre/all.json", 4)
	ctx := context.Background()

	if _, err := preparer.Prepare(ctx, prepare.Options{
		ProfileDir:  profileDir,
		VersionURL:  server.URL + "/version.json",
		OS:          "linux",
		Arch:        "x86_64",
		PlatformKey: "linux",
		LoaderName:  "vanilla",
	}); err != nil {
		t.Fatalf("prepare: %v", err)
	}

	for _, rel := range []string{
		"versions/1.21.1/1.21.1.jar",
		"libraries/com/example/x/1/x-1.jar",
		"assets/indexes/17.json",
		"assets/objects/" + assetHash[:2] + "/" + assetHash,
		"runtime/linux/bin/java",
		manifest.LaunchProfileName,
	} {
		if _, err := os.Stat(filepath.Join(profileDir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}

	data, _ := os.ReadFile(filepath.Join(profileDir, manifest.LaunchProfileName))
	var launch manifest.LaunchProfile
	if err := json.Unmarshal(data, &launch); err != nil {
		t.Fatal(err)
	}
	if launch.MainClass != "net.minecraft.client.main.Main" || launch.JavaMajor != 21 {
		t.Fatalf("launch = %+v", launch)
	}
	if len(launch.Classpath) != 2 {
		t.Fatalf("classpath = %v", launch.Classpath)
	}

	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	published, err := prepare.Publish(ctx, cas, manifest.NewSigner(priv), profileDir, "vanilla-1.21.1", "1")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if !manifest.Verify(pub, published.Canonical, published.Signature) {
		t.Fatal("published manifest signature invalid")
	}
	if len(published.Manifest.Files) < 6 {
		t.Fatalf("manifest files = %d", len(published.Manifest.Files))
	}
}
