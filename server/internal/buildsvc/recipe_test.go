package buildsvc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/compat"
	"github.com/laminara/laminara/server/internal/manifest"
)

type fakeRecipe struct {
	name    string
	summary string
	loader  string
}

func (f fakeRecipe) Name() string         { return f.name }
func (f fakeRecipe) Summary() string      { return f.summary }
func (f fakeRecipe) Loader() string       { return f.loader }
func (f fakeRecipe) Supports(string) bool { return true }
func (fakeRecipe) Versions(context.Context) ([]string, error) {
	return nil, nil
}
func (fakeRecipe) Resolve(context.Context, string, string) (*compat.Plan, error) {
	return nil, nil
}

func TestInstallerCanReadTheRecipeSectionOfLoaders(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("нет bash — раздел рецептов читает install.sh")
	}
	var out strings.Builder
	writeRecipes(&out, []compat.Recipe{fakeRecipe{name: "lwjgl3ify", summary: "Minecraft 1.7.10 на LWJGL 3", loader: "forge"}})

	listing := "vanilla    без модов\nforge      последняя 10.13.4.1614\n" + out.String()
	script := filepath.Join(repoRoot(t), "install.sh")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("install.sh рядом не нашёлся: %v", err)
	}

	shell := `
		set -eu
		eval "$(sed -n '/^recipe_section()/,/^first_build()/p' "$1" | sed '$d')"
		printf 'name=%s\n' "$(recipe_for "$2" forge)"
		printf 'vanilla=%s\n' "$(recipe_for "$2" vanilla)"
		printf 'summary=%s\n' "$(recipe_summary_for "$2" lwjgl3ify)"
	`
	command := exec.Command("bash", "-c", shell, "bash", script, listing)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh не разобрал вывод loaders: %v\n%s", err, output)
	}
	for _, want := range []string{"name=lwjgl3ify", "vanilla=\n", "summary=Minecraft 1.7.10 на LWJGL 3"} {
		if !strings.Contains(string(output), want) {
			t.Errorf("в разборе нет %q:\n%s", want, output)
		}
	}
}

func TestRecipeIsSkippedWhenTheOperatorSaysNo(t *testing.T) {
	for _, spec := range []string{"", "нет", "НЕТ", "none", "off", "no"} {
		if !isRecipeOff(spec) {
			t.Errorf("%q должно означать «без рецепта»", spec)
		}
	}
	if isRecipeOff("lwjgl3ify") {
		t.Error("имя рецепта не должно читаться как отказ")
	}
}

func TestRecipeSpecSplitsIntoNameAndVersion(t *testing.T) {
	name, version := compat.Split("lwjgl3ify:3.0.33")
	if name != "lwjgl3ify" || version != "3.0.33" {
		t.Fatalf("разбор дал %q %q", name, version)
	}
	name, version = compat.Split("lwjgl3ify")
	if name != "lwjgl3ify" || version != "" {
		t.Fatalf("без версии разбор дал %q %q", name, version)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 6; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Skip("корень репозитория не нашёлся")
	return ""
}

func settingsWith(t *testing.T, root, compat string, files []string) {
	t.Helper()
	if err := manifest.EnsureDefaultSettings(root); err != nil {
		t.Fatal(err)
	}
	if err := manifest.UpdateSettings(root, func(s *manifest.Settings) {
		s.Compat = compat
		s.CompatFiles = files
	}); err != nil {
		t.Fatal(err)
	}
}

func TestARepeatedInstallKeepsThePinnedRecipeVersion(t *testing.T) {
	profiles := t.TempDir()
	root := filepath.Join(profiles, "retro")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	settingsWith(t, root, "выдумка:1.0.0", nil)

	service := &Service{profilesDir: profiles}
	var out strings.Builder
	if _, err := service.compatPlan(context.Background(), "retro", map[string]string{"compat": "выдумка"}, "1.7.10", &out); err == nil {
		t.Fatal("рецепта «выдумка» нет, ожидалась ошибка")
	} else if !strings.Contains(err.Error(), "выдумка") {
		t.Fatalf("ошибка не называет рецепт: %v", err)
	}

	spec, newest := wantsNewest("выдумка:последняя")
	if spec != "выдумка" || !newest {
		t.Fatalf("«последняя» должна означать свежий релиз, получено %q %v", spec, newest)
	}
	if spec, newest := wantsNewest("выдумка:1.0.1"); spec != "выдумка:1.0.1" || newest {
		t.Fatalf("точная версия не должна читаться как «последняя»: %q %v", spec, newest)
	}
}

func TestABrokenSettingsFileIsNotReadAsAbsenceOfARecipe(t *testing.T) {
	profiles := t.TempDir()
	root := filepath.Join(profiles, "retro")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, manifest.SettingsFileName), []byte("{,}"), 0o644); err != nil {
		t.Fatal(err)
	}

	service := &Service{profilesDir: profiles}
	if _, err := service.rememberedRecipe("retro"); err == nil {
		t.Fatal("испорченные настройки не должны молча означать «рецепта нет» — сборка пересоберётся ванильной")
	}
}

func TestOnlyRecipeFilesUnderModsAreRemoved(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "mods"), 0o755); err != nil {
		t.Fatal(err)
	}
	jar := filepath.Join(root, "mods", "recipe.jar")
	if err := os.WriteFile(jar, []byte("мод"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	if err := removeStaleRecipeFile(root, "config", &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "config")); err != nil {
		t.Fatal("папка config не рецептовая — трогать её нельзя, даже если её вписали в настройки руками")
	}
	if err := removeStaleRecipeFile(root, "mods/recipe.jar", &out); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jar); !os.IsNotExist(err) {
		t.Fatal("мод прошлого рецепта должен уйти")
	}
}
