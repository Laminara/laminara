package prepare

import (
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
