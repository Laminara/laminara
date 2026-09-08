package clientconfig

import (
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func TestFolderComesFromProjectName(t *testing.T) {
	cfg := &config.Config{Branding: &config.BrandingConfig{Name: "Мир Приключений"}}
	if got := StorageNameFor(cfg); got != "Мир Приключений" {
		t.Fatalf("папка должна называться по проекту, получили %q", got)
	}
}

func TestExplicitFolderWins(t *testing.T) {
	cfg := &config.Config{Branding: &config.BrandingConfig{Name: "Мир Приключений", FolderName: "magicworld"}}
	if got := StorageNameFor(cfg); got != "magicworld" {
		t.Fatalf("заданное имя должно побеждать, получили %q", got)
	}
}

func TestWindowTitleIsTheFallback(t *testing.T) {
	cfg := &config.Config{Branding: &config.BrandingConfig{WindowTitle: "Прогон"}}
	if got := StorageNameFor(cfg); got != "Прогон" {
		t.Fatalf("без названия берётся заголовок окна, получили %q", got)
	}
}

func TestWithoutBrandingStaysLaminara(t *testing.T) {
	if got := StorageNameFor(&config.Config{}); got != "laminara" {
		t.Fatalf("без оформления имя должно оставаться прежним, получили %q", got)
	}
}

func TestPathTricksAreStripped(t *testing.T) {
	for _, raw := range []string{"../../etc", "a/b", `c\d`, "имя:двоеточие", "...."} {
		cfg := &config.Config{Branding: &config.BrandingConfig{FolderName: raw}}
		got := StorageNameFor(cfg)
		for _, bad := range []string{"/", `\`, ":", "..", "*", "?"} {
			if got != "laminara" && contains(got, bad) {
				t.Fatalf("%q превратилось в %q — так можно вылезти из своей папки", raw, got)
			}
		}
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
