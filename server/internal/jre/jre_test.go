package jre_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/laminara/laminara/server/internal/jre"
)

func TestSelectAndFetchFiles(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/runtime.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{
			"files": {
				"bin/java": { "type": "file", "executable": true, "downloads": { "raw": { "sha1": "aa", "size": 100, "url": "https://example/java" } } },
				"lib/rt": { "type": "file", "executable": false, "downloads": { "raw": { "sha1": "bb", "size": 200, "url": "https://example/rt" } } },
				"lib": { "type": "directory" }
			}
		}`)
	})
	mux.HandleFunc("/all.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"linux": {
				"java-runtime-delta": [
					{ "version": { "name": "21.0.3" }, "manifest": { "sha1": "cc", "size": 10, "url": "%s/runtime.json" } }
				]
			}
		}`, server.URL)
	})

	client := jre.NewClientWith(server.Client(), server.URL+"/all.json")
	ctx := context.Background()

	runtime, err := client.Select(ctx, "linux", "java-runtime-delta")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if runtime.Version.Name != "21.0.3" {
		t.Fatalf("version = %q", runtime.Version.Name)
	}

	files, err := client.FetchFiles(ctx, runtime.Manifest.URL)
	if err != nil {
		t.Fatalf("fetch files: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("files = %+v", files)
	}
	if files[0].Path != "bin/java" || !files[0].Executable {
		t.Fatalf("bin/java = %+v", files[0])
	}
	if files[1].Path != "lib/rt" || files[1].Executable {
		t.Fatalf("lib/rt = %+v", files[1])
	}
}

func TestSelectWithoutMojangRuntime(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/all.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"linux": {"java-runtime-delta": [ { "version": { "name": "21.0.3" } } ]}}`)
	})

	client := jre.NewClientWith(server.Client(), server.URL+"/all.json")
	if _, err := client.Select(context.Background(), "linux", "java-runtime-delta"); err != nil {
		t.Fatalf("select linux: %v", err)
	}
	_, err := client.Select(context.Background(), "linux-arm64", "java-runtime-delta")
	if !errors.Is(err, jre.ErrNoMojangRuntime) {
		t.Fatalf("select linux-arm64 = %v, want ErrNoMojangRuntime: Mojang ships no such runtime", err)
	}
}
