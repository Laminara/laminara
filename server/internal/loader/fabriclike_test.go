package loader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFabricLikeVersionsAndResolve(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/versions/loader/1.21.1", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `[
			{"loader":{"version":"0.16.0"},"launcherMeta":{}},
			{"loader":{"version":"0.15.11"},"launcherMeta":{}}
		]`)
	})
	mux.HandleFunc("/versions/loader/1.21.1/0.16.0", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{
			"loader":{"version":"0.16.0"},
			"launcherMeta":{
				"mainClass":{"client":"net.fabricmc.loader.impl.launch.knot.KnotClient"},
				"libraries":{
					"common":[{"name":"net.fabricmc:fabric-loader:0.16.0","url":"https://maven.fabricmc.net/"}],
					"client":[{"name":"org.ow2.asm:asm:9.7","url":"https://maven.fabricmc.net/"}]
				}
			}
		}`)
	})

	f := newFabricLike("fabric", server.URL)
	ctx := context.Background()

	versions, err := f.Versions(ctx, "1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 2 || versions[0] != "0.16.0" {
		t.Fatalf("versions = %v", versions)
	}

	profile, err := f.Resolve(ctx, "1.21.1", "0.16.0")
	if err != nil {
		t.Fatal(err)
	}
	if profile.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("mainClass = %q", profile.MainClass)
	}
	if len(profile.Libraries) != 2 {
		t.Fatalf("libraries = %+v", profile.Libraries)
	}
}
