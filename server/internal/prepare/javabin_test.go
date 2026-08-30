package prepare

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/jre"
)

func TestResolveJavaBinPerPlatform(t *testing.T) {
	cases := []struct {
		name     string
		key      string
		files    []string
		expected string
	}{
		{"linux", "linux", []string{"bin/java", "lib/modules", "legal/LICENSE"}, "runtime/linux/bin/java"},
		{"windows", "windows-x64", []string{"bin/java.exe", "bin/javaw.exe", "lib/modules"}, "runtime/windows-x64/bin/javaw.exe"},
		{
			"macos",
			"mac-os-arm64",
			[]string{"jre.bundle/Contents/Info.plist", "jre.bundle/Contents/Home/bin/java", "jre.bundle/Contents/Home/lib/modules"},
			"runtime/mac-os-arm64/jre.bundle/Contents/Home/bin/java",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := make([]jre.RuntimeFile, 0, len(tc.files))
			for _, path := range tc.files {
				files = append(files, jre.RuntimeFile{Path: path})
			}
			got, err := resolveJavaBin(files, tc.key)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if got != tc.expected {
				t.Fatalf("got %q, want %q", got, tc.expected)
			}
		})
	}

	if _, err := resolveJavaBin([]jre.RuntimeFile{{Path: "lib/modules"}}, "linux"); err == nil {
		t.Fatal("a runtime without java must error")
	}
}

func TestDownloadRuntimeExplainsWhereToTakeJavaFrom(t *testing.T) {
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	defer server.Close()
	mux.HandleFunc("/all.json", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"linux": {"java-runtime-delta": [ { "version": { "name": "21.0.3" } } ]}}`)
	})

	preparer := NewPreparerWith(server.Client(), "", server.URL+"/all.json", 1)
	_, err := preparer.downloadRuntime(context.Background(), t.TempDir(), "linux-arm64", "java-runtime-delta")
	if err == nil {
		t.Fatal("a platform Mojang ships no runtime for must fail with a hint")
	}
	for _, want := range []string{"нет готовой Java", "linux-arm64", "JDK", "laminara.profile.json"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("hint %q does not mention %q", err.Error(), want)
		}
	}
}
