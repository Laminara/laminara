package buildsvc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/laminara/laminara/server/internal/buildview"
	"github.com/laminara/laminara/server/internal/compat"
	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/safepath"
)

const (
	recipeOff     = "нет"
	recipeNewest  = "последняя"
	recipeFiles   = "mods/"
	recipesHeader = "Рецепты совместимости — install <имя> <версия> compat=<рецепт>:"
)

func writeRecipes(out io.Writer, recipes []compat.Recipe) {
	if len(recipes) == 0 {
		return
	}
	fmt.Fprintln(out, "\n"+recipesHeader)
	for _, recipe := range recipes {
		fmt.Fprintf(out, "%-10s %-9s %s\n", recipe.Name(), recipe.Loader(), recipe.Summary())
	}
}

func (s *Service) compatPlan(ctx context.Context, name string, opts map[string]string, mcVersion string, out io.Writer) (*compat.Plan, error) {
	spec, asked := opts["compat"]
	remembered, err := s.rememberedRecipe(name)
	if err != nil {
		return nil, err
	}
	switch {
	case asked && isRecipeOff(spec):
		return nil, nil
	case !asked && remembered == "":
		return nil, nil
	case !asked:
		spec = remembered
	}

	spec, newest := wantsNewest(spec)
	if wanted, pinned := compat.Split(spec); pinned == "" && !newest {
		if was, version := compat.Split(remembered); was == wanted && version != "" {
			spec = remembered
		}
	}

	plan, err := compat.Resolve(ctx, spec, mcVersion)
	if err != nil {
		return nil, recipeHint(err, spec, asked)
	}
	switch {
	case remembered == "":
		if _, pinned := compat.Split(spec); pinned == "" {
			fmt.Fprintf(out, "Версия рецепта %s запомнена в настройках сборки — повторный install соберёт ту же.\n", plan.Version)
		}
	case remembered != plan.Recipe+":"+plan.Version:
		_, was := compat.Split(remembered)
		fmt.Fprintf(out, "Версия рецепта меняется с %s на %s — сборка получится другой, чем прошлая.\n", was, plan.Version)
	}
	return plan, nil
}

func wantsNewest(spec string) (string, bool) {
	name, version := compat.Split(spec)
	switch strings.ToLower(version) {
	case recipeNewest, "latest", "новая", "свежая":
		return name, true
	}
	return spec, false
}

func recipeHint(err error, spec string, asked bool) error {
	name, _ := compat.Split(spec)
	if _, known := compat.Get(name); !known {
		if _, isLoader := loader.Get(name); isLoader {
			return fmt.Errorf("%s — это загрузчик, а не рецепт: напишите loader=%s", name, name)
		}
		return err
	}
	if !asked {
		return fmt.Errorf("%w; рецепт запомнен от прошлой сборки — соберите без него: compat=%s", err, recipeOff)
	}
	return err
}

func (s *Service) rememberedRecipe(name string) (string, error) {
	settings, err := manifest.LoadSettings(filepath.Join(s.profilesDir, name))
	if err != nil {
		return "", fmt.Errorf("настройки сборки «%s» не читаются, а в них записан рецепт: %w", name, err)
	}
	return settings.Compat, nil
}

func (s *Service) rememberRecipe(root string, plan *compat.Plan, out io.Writer) error {
	settings, err := manifest.LoadSettings(root)
	if err != nil {
		return err
	}
	wanted, files := "", []string(nil)
	if plan != nil {
		wanted = plan.Recipe + ":" + plan.Version
		for _, file := range plan.Files {
			files = append(files, file.Path)
		}
	}
	if settings.Compat == wanted && slices.Equal(settings.CompatFiles, files) {
		return nil
	}
	if err := manifest.UpdateSettings(root, func(settings *manifest.Settings) {
		settings.Compat = wanted
		settings.CompatFiles = files
	}); err != nil {
		return err
	}
	for _, stale := range settings.CompatFiles {
		if slices.Contains(files, stale) {
			continue
		}
		if err := removeStaleRecipeFile(root, stale, out); err != nil {
			return err
		}
	}
	return nil
}

func removeStaleRecipeFile(root, path string, out io.Writer) error {
	if !strings.HasPrefix(path, recipeFiles) {
		return nil
	}
	full, err := safepath.Join(root, path)
	if err != nil {
		return err
	}
	info, err := os.Lstat(full)
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	if err := os.Remove(full); err != nil {
		return err
	}
	fmt.Fprintf(out, "Убрал из сборки %s — его приносил прошлый рецепт.\n", path)
	return nil
}

func announceRecipe(out io.Writer, plan *compat.Plan, askedLoader, askedVersion string) {
	named := buildview.LoaderWord(plan.LoaderName)
	if plan.LoaderVersion != "" {
		named += " " + plan.LoaderVersion
	}
	fmt.Fprintf(out, "Рецепт %s %s: Minecraft через %s и Java %d.\n", plan.Recipe, plan.Version, named, plan.JavaMajor)
	if askedLoader != "" && askedLoader != plan.LoaderName {
		fmt.Fprintf(out, "Загрузчик %s не подойдёт — рецепт собран под %s, беру его.\n", askedLoader, plan.LoaderName)
	}
	if askedVersion != "" && plan.LoaderVersion != "" && askedVersion != plan.LoaderVersion {
		fmt.Fprintf(out, "Версия загрузчика %s заменена на %s: рецепт пропатчен именно под неё.\n", askedVersion, plan.LoaderVersion)
	}
	if plan.Prerelease {
		fmt.Fprintf(out, "Рецепт %s %s — предварительная версия, сбои в ней ожидаемы.\n", plan.Recipe, plan.Version)
	}
	for _, file := range plan.Files {
		fmt.Fprintf(out, "Кладу в сборку %s.\n", file.Path)
	}
	for _, note := range plan.Notes {
		fmt.Fprintln(out, note)
	}
}

func planFiles(plan *compat.Plan) []compat.File {
	if plan == nil {
		return nil
	}
	return plan.Files
}

func isRecipeOff(spec string) bool {
	switch strings.ToLower(strings.TrimSpace(spec)) {
	case "", recipeOff, "no", "none", "off":
		return true
	}
	return false
}
