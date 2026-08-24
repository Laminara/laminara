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
			"loader":{"version":"0.16.0","maven":"net.fabricmc:fabric-loader:0.16.0"},
			"intermediary":{"maven":"net.fabricmc:intermediary:1.21.1"},
			"launcherMeta":{
				"mainClass":{"client":"net.fabricmc.loader.impl.launch.knot.KnotClient"},
				"libraries":{
					"common":[{"name":"org.ow2.asm:asm:9.7","url":"https://maven.fabricmc.net/"}],
					"client":[{"name":"net.fabricmc:sponge-mixin:0.15.4","url":"https://maven.fabricmc.net/"}]
				}
			}
		}`)
	})

	f := newFabricLike("fabric", server.URL, fabricMaven)
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
	if len(profile.Libraries) != 4 {
		t.Fatalf("libraries = %+v", profile.Libraries)
	}
	if profile.Libraries[0].Name != "net.fabricmc:intermediary:1.21.1" {
		t.Fatalf("intermediary is missing from the classpath: %+v", profile.Libraries)
	}
	if profile.Libraries[1].Name != "net.fabricmc:fabric-loader:0.16.0" {
		t.Fatalf("the loader itself is missing from the classpath: %+v", profile.Libraries)
	}
}

func TestQuiltLikeTakesItsLoaderFromItsOwnMaven(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	mux.HandleFunc("/versions/loader/1.21.1/0.20.0", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{
			"loader":{"version":"0.20.0","maven":"org.quiltmc:quilt-loader:0.20.0"},
			"intermediary":{"maven":"net.fabricmc:intermediary:1.21.1"},
			"launcherMeta":{"mainClass":{"client":"org.quiltmc.loader.impl.launch.knot.KnotClient"},"libraries":{}}
		}`)
	})

	quiltMaven := "https://maven.quiltmc.org/repository/release/"
	profile, err := newFabricLike("quilt", server.URL, quiltMaven).Resolve(context.Background(), "1.21.1", "0.20.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(profile.Libraries) != 2 {
		t.Fatalf("libraries = %+v", profile.Libraries)
	}
	if profile.Libraries[0].URL != fabricMaven {
		t.Fatalf("intermediary must come from the fabric maven: %+v", profile.Libraries[0])
	}
	if profile.Libraries[1].URL != quiltMaven {
		t.Fatalf("the quilt loader must come from the quilt maven: %+v", profile.Libraries[1])
	}
}
