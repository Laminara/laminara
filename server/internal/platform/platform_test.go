package platform_test

import (
	"testing"

	"github.com/laminara/laminara/server/internal/platform"
)

func TestKeyFromRuntime(t *testing.T) {
	cases := map[[2]string]string{
		{"linux", "amd64"}:   "linux",
		{"linux", "386"}:     "linux-i386",
		{"linux", "arm64"}:   "linux-arm64",
		{"darwin", "amd64"}:  "mac-os",
		{"darwin", "arm64"}:  "mac-os-arm64",
		{"windows", "amd64"}: "windows-x64",
		{"windows", "386"}:   "windows-x86",
		{"windows", "arm64"}: "windows-arm64",
	}
	for input, want := range cases {
		key, ok := platform.Key(platform.FromRuntime(input[0], input[1]))
		if !ok || key != want {
			t.Fatalf("Key(FromRuntime(%q,%q)) = %q, %v; want %q", input[0], input[1], key, ok, want)
		}
	}
}

func TestMojangVocabulary(t *testing.T) {
	for _, p := range platform.Game() {
		key, _ := platform.Key(p)
		os, arch, ok := platform.Mojang(p)
		if key == "linux-arm64" {
			if ok {
				t.Fatal("Mojang(linux-arm64) ok, want false: Mojang ships no such flavour")
			}
			continue
		}
		if !ok {
			t.Fatalf("Mojang(%s) has no pair", key)
		}
		if wantMac := key == "mac-os" || key == "mac-os-arm64"; (os == "osx") != wantMac {
			t.Fatalf("Mojang(%s) os = %q", key, os)
		}
		if arch != "x86_64" && arch != "x86" && arch != "arm64" {
			t.Fatalf("Mojang(%s) arch = %q", key, arch)
		}
	}
}

func TestParseRoundTrip(t *testing.T) {
	for _, p := range platform.Game() {
		key, ok := platform.Key(p)
		if !ok {
			t.Fatalf("Key(%v) not found", p)
		}
		back, ok := platform.Parse(key)
		if !ok || back != p {
			t.Fatalf("Parse(%q) = %v, %v; want %v", key, back, ok, p)
		}
	}
	if _, ok := platform.Parse("plan9"); ok {
		t.Fatal("Parse(plan9) ok, want false")
	}
}
