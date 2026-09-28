package buildsvc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/compat"
	"github.com/laminara/laminara/server/internal/manifest"
)

func TestRecordedInstallIsAValidInstallCommand(t *testing.T) {
	cases := []struct {
		name          string
		version       string
		loader        string
		loaderVersion string
		plan          *compat.Plan
		java          string
		want          map[string]string
	}{
		{name: "vanilla", version: "1.21.1", want: map[string]string{}},
		{name: "named vanilla", version: "1.21.1", loader: "vanilla", want: map[string]string{}},
		{name: "fabric", version: "1.21.1", loader: "fabric", loaderVersion: "0.16.5", want: map[string]string{"loader": "fabric", "loaderVersion": "0.16.5"}},
		{name: "recipe", version: "1.7.10", loader: "forge", loaderVersion: "10.13.4.1614", plan: &compat.Plan{Recipe: "lwjgl3ify", Version: "3.0.33"}, want: map[string]string{"compat": "lwjgl3ify:3.0.33"}},
		{name: "own java", version: "1.7.10", loader: "forge", loaderVersion: "10.13.4.1614", java: "java-runtime-delta", want: map[string]string{"loader": "forge", "loaderVersion": "10.13.4.1614", "java": "java-runtime-delta"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := strings.Fields(installLine(c.version, c.loader, c.loaderVersion, c.plan, c.java))
			if len(args) == 0 || args[0] != c.version {
				t.Fatalf("the line must start with the Minecraft version: %q", args)
			}
			got := parseKV(args[1:])
			if len(got) != len(args)-1 {
				t.Fatalf("every word after the version must be key=value: %q", args)
			}
			if len(got) != len(c.want) {
				t.Fatalf("got %v, want %v", got, c.want)
			}
			for key, value := range c.want {
				if got[key] != value {
					t.Errorf("%s = %q, want %q", key, got[key], value)
				}
			}
		})
	}
}

func TestInstallWarnsAboutAHandMadeLaunchNextToARecipe(t *testing.T) {
	var out strings.Builder
	warnHandMadeLaunch(&out, manifest.Settings{
		JvmArgs:          []string{"-Dfile.encoding=UTF-8"},
		Classpath:        []string{"libraries/lwjgl3ify/lwjgl3ify-3.0.32-forgePatches.jar"},
		ClasspathExclude: []string{"libraries/org/lwjgl/lwjgl/*"},
		MainClass:        "com.gtnewhorizons.retrofuturabootstrap.MainStartOnFirstThread",
	})
	for _, want := range []string{"classpath,", "classpathExclude", "mainClass", manifest.SettingsFileName} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("warning must name %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "jvmArgs") {
		t.Errorf("jvmArgs are legitimate next to a recipe and must not be flagged:\n%s", out.String())
	}

	out.Reset()
	warnHandMadeLaunch(&out, manifest.Settings{MainClass: "com.example.Boot"})
	if !strings.Contains(out.String(), "поле mainClass") {
		t.Errorf("one field must be named in the singular:\n%s", out.String())
	}

	out.Reset()
	warnHandMadeLaunch(&out, manifest.Settings{JvmArgs: []string{"-Xss4m"}})
	if out.Len() != 0 {
		t.Errorf("nothing to warn about, got:\n%s", out.String())
	}
}

const pinnedMinecraft = "0.0-тест"

type pinnedRecipe struct {
	latest string
}

func (pinnedRecipe) Name() string                             { return "тест-закреплённый" }
func (pinnedRecipe) Summary() string                          { return "рецепт для теста" }
func (pinnedRecipe) Loader() string                           { return "forge" }
func (pinnedRecipe) Supports(mcVersion string) bool           { return mcVersion == pinnedMinecraft }
func (r pinnedRecipe) Latest(context.Context) (string, error) { return r.latest, nil }
func (r pinnedRecipe) Resolve(_ context.Context, _ string, version string) (*compat.Plan, error) {
	if version == "" {
		version = r.latest
	}
	return &compat.Plan{Recipe: r.Name(), Version: version, LoaderName: "forge"}, nil
}

func TestPinnedRecipeMentionsANewerRelease(t *testing.T) {
	recipe := pinnedRecipe{latest: "2.0"}
	compat.Register(recipe)
	service := &Service{profilesDir: t.TempDir()}
	writeRecipeSettings(t, service, "pack", recipe.Name()+":1.0")

	var out strings.Builder
	plan, err := service.compatPlan(context.Background(), "pack", map[string]string{}, pinnedMinecraft, &out)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != "1.0" {
		t.Fatalf("a remembered recipe must stay pinned, got %s", plan.Version)
	}
	if !strings.Contains(out.String(), "2.0") || !strings.Contains(out.String(), "compat="+recipe.Name()+":"+recipeNewest) {
		t.Errorf("pinned recipe must point at the newer release and how to take it:\n%s", out.String())
	}

	out.Reset()
	if _, err := service.compatPlan(context.Background(), "pack", map[string]string{"compat": recipe.Name()}, pinnedMinecraft, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "2.0") {
		t.Errorf("a bare recipe name keeps the pin, so it must hint too:\n%s", out.String())
	}

	out.Reset()
	if _, err := service.compatPlan(context.Background(), "pack", map[string]string{"compat": recipe.Name() + ":1.0"}, pinnedMinecraft, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "2.0") {
		t.Errorf("a version the operator typed out is a choice, not something to nag about:\n%s", out.String())
	}

	out.Reset()
	writeRecipeSettings(t, service, "pack", recipe.Name()+":2.0")
	if _, err := service.compatPlan(context.Background(), "pack", map[string]string{}, pinnedMinecraft, &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), recipeNewest) {
		t.Errorf("an up-to-date recipe needs no hint:\n%s", out.String())
	}
}

func writeRecipeSettings(t *testing.T, service *Service, name, recipe string) {
	t.Helper()
	root := filepath.Join(service.profilesDir, name)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := manifest.EnsureDefaultSettings(root); err != nil {
		t.Fatal(err)
	}
	if err := manifest.UpdateSettings(root, func(settings *manifest.Settings) { settings.Compat = recipe }); err != nil {
		t.Fatal(err)
	}
}
