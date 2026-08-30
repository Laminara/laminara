package maven_test

import (
	"testing"

	"github.com/laminara/laminara/server/internal/maven"
)

func TestPath(t *testing.T) {
	cases := map[string]string{
		"net.neoforged:neoforge:21.1.235":                           "net/neoforged/neoforge/21.1.235/neoforge-21.1.235.jar",
		"net.neoforged:neoforge:21.1.235:client":                    "net/neoforged/neoforge/21.1.235/neoforge-21.1.235-client.jar",
		"net.neoforged:neoform:1.21.1-20240808.144430:mappings@txt": "net/neoforged/neoform/1.21.1-20240808.144430/neoform-1.21.1-20240808.144430-mappings.txt",
		"org.ow2.asm:asm:9.6@zip":                                   "org/ow2/asm/asm/9.6/asm-9.6.zip",
	}
	for coords, want := range cases {
		got, err := maven.Path(coords)
		if err != nil || got != want {
			t.Fatalf("maven.Path(%q) = %q, %v; want %q", coords, got, err, want)
		}
	}
}

func TestPathInvalid(t *testing.T) {
	for _, coords := range []string{"", "net.neoforged", "net.neoforged:neoforge"} {
		if _, err := maven.Path(coords); err == nil {
			t.Fatalf("maven.Path(%q) = nil error, want error", coords)
		}
	}
}
