package compat

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const modJar = "мод lwjgl3ify"

func fakeGitHub(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	sum := sha256.Sum256([]byte(modJar))
	digest := hex.EncodeToString(sum[:])

	mux.HandleFunc("/repos/GTNewHorizons/lwjgl3ify/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name": "3.0.33",
			"prerelease": false,
			"assets": [
				{"name": "version.json", "browser_download_url": "%s/manifest", "digest": "sha256:%s"},
				{"name": "lwjgl3ify-3.0.33.jar", "browser_download_url": "%s/mod", "digest": "sha256:%s"}
			]
		}`, server.URL, digest, server.URL, digest)
	})
	mux.HandleFunc("/repos/LegacyModdingMC/UniMixins/releases/latest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `{
			"tag_name": "0.3.1",
			"assets": [
				{"name": "+unimixins-all-1.7.10-0.3.1-dev.jar", "browser_download_url": "%s/mixins-dev"},
				{"name": "+unimixins-all-1.7.10-0.3.1.jar", "browser_download_url": "%s/mixins", "digest": "sha256:%s"}
			]
		}`, server.URL, server.URL, digest)
	})
	mux.HandleFunc("/manifest", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{
			"id": "1.7.10-Forge10.13.4.1614-1.7.10-lwjgl3ify-3.0.33",
			"mainClass": "com.gtnewhorizons.retrofuturabootstrap.MainStartOnFirstThread",
			"javaVersion": {"component": "java-runtime-epsilon", "majorVersion": 25}
		}`)
	})
	return server
}

func TestRecipeTakesForgeAndJavaFromItsOwnManifest(t *testing.T) {
	server := fakeGitHub(t)
	recipe := newLwjgl3ifyWith(server.URL, server.Client())

	plan, err := recipe.Resolve(context.Background(), "1.7.10", "")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != "3.0.33" {
		t.Errorf("версия рецепта = %q", plan.Version)
	}
	if plan.LoaderName != "forge" || plan.LoaderVersion != "10.13.4.1614" {
		t.Errorf("загрузчик рецепта = %q %q, а он записан в id манифеста", plan.LoaderName, plan.LoaderVersion)
	}
	if plan.JavaMajor != 25 {
		t.Errorf("Java рецепта = %d, манифест просит 25", plan.JavaMajor)
	}
	if plan.VersionURL != server.URL+"/manifest" {
		t.Errorf("сборка пойдёт не по манифесту рецепта: %q", plan.VersionURL)
	}
}

func TestRecipeBringsItsModAndUniMixinsWithChecksums(t *testing.T) {
	server := fakeGitHub(t)
	recipe := newLwjgl3ifyWith(server.URL, server.Client())

	plan, err := recipe.Resolve(context.Background(), "1.7.10", "")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range plan.Files {
		paths = append(paths, file.Path)
		if file.SHA256 == "" {
			t.Errorf("%s кладётся без контрольной суммы", file.Path)
		}
	}
	want := []string{"mods/lwjgl3ify-3.0.33.jar", "mods/+unimixins-all-1.7.10-0.3.1.jar"}
	if strings.Join(paths, " ") != strings.Join(want, " ") {
		t.Fatalf("в сборку кладётся %v, а без UniMixins lwjgl3ify не стартует", paths)
	}
}

func TestRecipeRefusesAVersionItWasNotBuiltFor(t *testing.T) {
	server := fakeGitHub(t)
	recipe := newLwjgl3ifyWith(server.URL, server.Client())

	if _, err := Resolve(context.Background(), "lwjgl3ify", "1.12.2"); err == nil {
		t.Fatal("рецепт 1.7.10 не должен браться за 1.12.2")
	}
	if !recipe.Supports("1.7.10") {
		t.Fatal("рецепт обязан поддерживать 1.7.10")
	}
}

func TestForgeVersionIsReadFromTheManifestID(t *testing.T) {
	cases := map[string]string{
		"1.7.10-Forge10.13.4.1614-1.7.10-lwjgl3ify-3.0.33": "10.13.4.1614",
		"1.7.10-lwjgl3ify-3.0.33":                          "",
	}
	for id, want := range cases {
		if got := forgeVersionOf(id); got != want {
			t.Errorf("%s -> %q, ожидалось %q", id, got, want)
		}
	}
}
