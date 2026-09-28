package buildsvc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/laminara/laminara/server/internal/buildview"
	"github.com/laminara/laminara/server/internal/compat"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/platform"
)

func installLine(mcVersion, loaderName, loaderVersion string, plan *compat.Plan, java string) string {
	words := []string{mcVersion}
	switch {
	case plan != nil:
		words = append(words, "compat="+plan.Recipe+":"+plan.Version)
	case buildview.LoaderWord(loaderName) != "vanilla":
		words = append(words, "loader="+loaderName)
		if loaderVersion != "" {
			words = append(words, "loaderVersion="+loaderVersion)
		}
	}
	if java != "" {
		words = append(words, "java="+java)
	}
	return strings.Join(words, " ")
}

func recordInstall(root, line string) error {
	if err := os.MkdirAll(root, 0o750); err != nil {
		return err
	}
	if err := manifest.EnsureDefaultSettings(root); err != nil {
		return err
	}
	return manifest.UpdateSettings(root, func(settings *manifest.Settings) { settings.Install = line })
}

func refuseStalePlatforms(name string, layout buildLayout) error {
	if layout.flat {
		return nil
	}
	settings, err := manifest.LoadSettings(layout.root)
	if err != nil {
		return err
	}
	if settings.Install == "" {
		return nil
	}
	var stale []string
	for _, variant := range layout.platforms {
		launch, err := manifest.ReadLaunchProfile(layout.place(variant).platform)
		if err != nil {
			return err
		}
		if launch.Install != settings.Install {
			key, _ := platform.Key(variant)
			stale = append(stale, key)
		}
	}
	if len(stale) == 0 {
		return nil
	}
	rebuild := buildview.InstallCommand(name, settings) + " platform=" + strings.Join(stale, ",")
	if len(stale) == len(layout.platforms) {
		return fmt.Errorf("последний install сборки «%s» не собрал ни одной платформы, а общие файлы мог уже поменять — повторите его: %s", name, rebuild)
	}
	refusal := "платформа %s собрана прошлым install и разошлась бы с остальной сборкой — пересоберите её: %s, а если она больше не нужна, удалите её папку из %s"
	if len(stale) > 1 {
		refusal = "платформы %s собраны прошлым install и разошлись бы с остальной сборкой — пересоберите их: %s, а ту, что больше не нужна, удалите из %s"
	}
	return fmt.Errorf(refusal, strings.Join(stale, ", "), rebuild, filepath.Dir(layout.place(layout.platforms[0]).platform))
}
