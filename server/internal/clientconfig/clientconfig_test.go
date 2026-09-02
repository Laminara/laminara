package clientconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/mediatype"
)

func TestInlineAssetPicksTheMediaTypeByExtension(t *testing.T) {
	dir := t.TempDir()
	png := filepath.Join(dir, "logo.png")
	if err := os.WriteFile(png, []byte("png-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	clip := filepath.Join(dir, "hero.mp4")
	if err := os.WriteFile(clip, []byte("mp4-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	mystery := filepath.Join(dir, "logo")
	if err := os.WriteFile(mystery, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path string
		want string
	}{
		{png, "data:image/png;base64,"},
		{clip, "data:video/mp4;base64,"},
		{mystery, "data:" + mediatype.Default + ";base64,"},
	} {
		got, err := inlineAsset(test.path)
		if err != nil {
			t.Fatalf("%s: %v", test.path, err)
		}
		if !strings.HasPrefix(got, test.want) {
			t.Fatalf("inlineAsset(%s) = %q, want prefix %q", test.path, got, test.want)
		}
	}
}

func TestInlineAssetWithoutAPathIsEmpty(t *testing.T) {
	if got, err := inlineAsset(""); err != nil || got != "" {
		t.Fatalf("inlineAsset(\"\") = %q, %v", got, err)
	}
}

func TestLauncherNameComesFromBranding(t *testing.T) {
	for _, test := range []struct {
		branding *Branding
		want     string
	}{
		{nil, "Laminara"},
		{&Branding{Name: "Magic World"}, "Magic World"},
		{&Branding{Name: "Magic", WindowTitle: "Magic World"}, "Magic World"},
		{&Branding{WindowTitle: `Ma/gic:World?`}, "MagicWorld"},
		{&Branding{WindowTitle: "   "}, "Laminara"},
	} {
		got := Document{Branding: test.branding}.LauncherName()
		if got != test.want {
			t.Fatalf("LauncherName(%+v) = %q, want %q", test.branding, got, test.want)
		}
	}
}
