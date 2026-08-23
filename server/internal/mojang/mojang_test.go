package mojang_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/laminara/laminara/server/internal/mojang"
)

func TestListAndFetchVersion(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/versions/1.21.1.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{
			"id": "1.21.1",
			"type": "release",
			"mainClass": "net.minecraft.client.main.Main",
			"javaVersion": { "component": "java-runtime-delta", "majorVersion": 21 },
			"assetIndex": { "id": "17", "url": "https://example/assets.json", "sha1": "aa", "size": 1, "totalSize": 2 },
			"downloads": { "client": { "sha1": "bb", "size": 100, "url": "https://example/client.jar" } }
		}`)
	})
	mux.HandleFunc("/manifest.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"latest": { "release": "1.21.1", "snapshot": "24w30a" },
			"versions": [
				{ "id": "1.21.1", "type": "release", "url": "%s/versions/1.21.1.json", "releaseTime": "2024-08-08T12:24:45+00:00", "sha1": "cc" },
				{ "id": "1.7.10", "type": "release", "url": "%s/versions/1.7.10.json", "releaseTime": "2014-05-14T17:29:23+00:00", "sha1": "dd" }
			]
		}`, server.URL, server.URL)
	})

	client := mojang.NewClientWith(server.Client(), server.URL+"/manifest.json")
	ctx := context.Background()

	manifest, err := client.ListVersions(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if manifest.Latest.Release != "1.21.1" {
		t.Fatalf("latest release = %q", manifest.Latest.Release)
	}
	if len(manifest.Versions) != 2 || manifest.Versions[0].ID != "1.21.1" {
		t.Fatalf("versions = %+v", manifest.Versions)
	}

	detail, err := client.FetchVersion(ctx, manifest.Versions[0].URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if detail.JavaVersion.MajorVersion != 21 {
		t.Fatalf("java major = %d", detail.JavaVersion.MajorVersion)
	}
	if detail.MainClass != "net.minecraft.client.main.Main" {
		t.Fatalf("mainClass = %q", detail.MainClass)
	}
	if detail.Downloads.Client.URL != "https://example/client.jar" || detail.Downloads.Client.Size != 100 {
		t.Fatalf("client download = %+v", detail.Downloads.Client)
	}
}
