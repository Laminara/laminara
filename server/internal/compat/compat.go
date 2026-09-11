package compat

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type File struct {
	Path   string
	URL    string
	SHA256 string
}

type Plan struct {
	Recipe        string
	Version       string
	VersionURL    string
	VersionID     string
	LoaderName    string
	LoaderVersion string
	JavaMajor     int
	Prerelease    bool
	Files         []File
	Notes         []string
}

type Recipe interface {
	Name() string
	Summary() string
	Loader() string
	Supports(mcVersion string) bool
	Versions(ctx context.Context) ([]string, error)
	Resolve(ctx context.Context, mcVersion, version string) (*Plan, error)
}

var registry = map[string]Recipe{}

func Register(recipe Recipe) {
	registry[recipe.Name()] = recipe
}

func Get(name string) (Recipe, bool) {
	recipe, ok := registry[name]
	return recipe, ok
}

func All() []Recipe {
	recipes := make([]Recipe, 0, len(registry))
	for _, recipe := range registry {
		recipes = append(recipes, recipe)
	}
	sort.Slice(recipes, func(i, j int) bool { return recipes[i].Name() < recipes[j].Name() })
	return recipes
}

func For(mcVersion string) []Recipe {
	var matched []Recipe
	for _, recipe := range All() {
		if recipe.Supports(mcVersion) {
			matched = append(matched, recipe)
		}
	}
	return matched
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Split(spec string) (string, string) {
	name, version, _ := strings.Cut(strings.TrimSpace(spec), ":")
	return name, version
}

func Resolve(ctx context.Context, spec, mcVersion string) (*Plan, error) {
	name, version := Split(spec)
	recipe, ok := Get(name)
	if !ok {
		return nil, fmt.Errorf("рецепта совместимости «%s» нет — есть: %s", name, strings.Join(Names(), ", "))
	}
	if !recipe.Supports(mcVersion) {
		return nil, fmt.Errorf("рецепт «%s» не для Minecraft %s — %s", name, mcVersion, recipe.Summary())
	}
	plan, err := recipe.Resolve(ctx, mcVersion, version)
	if err != nil {
		return nil, err
	}
	for _, file := range plan.Files {
		if file.SHA256 == "" {
			return nil, fmt.Errorf("у файла %s из рецепта %s %s нет контрольной суммы — без неё скачанное не проверить", file.Path, plan.Recipe, plan.Version)
		}
	}
	return plan, nil
}
