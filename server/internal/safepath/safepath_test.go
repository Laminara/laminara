package safepath

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestJoinKeepsHonestPaths(t *testing.T) {
	root := filepath.Join("srv", "profiles", "survival")
	for _, path := range []string{
		"mods/jei.jar",
		"./mods/jei.jar",
		"maven/net/minecraftforge/forge/1.21.1/forge.jar",
		"a//b/c.txt",
	} {
		got, err := Join(root, path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if !strings.HasPrefix(got, root) {
			t.Fatalf("%s ушёл из корня: %s", path, got)
		}
	}
}

func TestJoinRefusesEscapes(t *testing.T) {
	root := filepath.Join("srv", "libraries")
	for _, path := range []string{
		"../../etc/cron.d/backdoor",
		"maven/../../../root/.ssh/authorized_keys",
		"/etc/passwd",
		"C:/Windows/System32/evil.dll",
		`..\..\windows\system32\evil.dll`,
		"",
		"   ",
		".",
		"./",
	} {
		if got, err := Join(root, path); err == nil {
			t.Fatalf("путь %q должен быть отвергнут, а вышло %q", path, got)
		}
	}
}

func TestRelativeNormalises(t *testing.T) {
	got, err := Relative("a/b/../b/c.txt")
	if err == nil {
		t.Fatalf("даже безобидный .. в середине отвергаем, чтобы не разбирать крайние случаи: %q", got)
	}
	if got, err := Relative("a/./b.txt"); err != nil || got != filepath.Join("a", "b.txt") {
		t.Fatalf("Relative = %q, %v", got, err)
	}
}
