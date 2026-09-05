package control

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeDirPrefersExplicitOverride(t *testing.T) {
	t.Setenv("LAMINARA_RUNTIME_DIR", "/custom/laminara")
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if got := RuntimeDir(); got != "/custom/laminara" {
		t.Fatalf("RuntimeDir = %q, want the explicit override", got)
	}
}

func TestRuntimeDirUsesXDGWhenNoSystemDir(t *testing.T) {
	if _, err := os.Stat(systemRuntimeDir); err == nil {
		t.Skip("на этой машине есть /run/laminara — системный путь имеет приоритет")
	}
	if os.Geteuid() == 0 {
		t.Skip("под root системный путь имеет приоритет")
	}
	xdg := t.TempDir()
	t.Setenv("LAMINARA_RUNTIME_DIR", "")
	t.Setenv("XDG_RUNTIME_DIR", xdg)
	if got := RuntimeDir(); got != filepath.Join(xdg, "laminara") {
		t.Fatalf("RuntimeDir = %q, want %q", got, filepath.Join(xdg, "laminara"))
	}
}
