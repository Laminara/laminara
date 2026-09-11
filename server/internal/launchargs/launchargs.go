package launchargs

import (
	"strings"

	"github.com/laminara/laminara/server/internal/mojang"
)

var resolvablePlaceholders = []string{
	"${library_directory}",
	"${classpath_separator}",
	"${version_name}",
	"${natives_directory}",
}

var launcherJVMArgs = []string{
	"-XstartOnFirstThread",
	"-Djava.library.path=",
	"-Dorg.lwjgl.system.SharedLibraryExtractPath=",
	"-Dminecraft.launcher.brand=",
	"-Dminecraft.launcher.version=",
}

var launcherGameFlags = map[string]bool{
	"--username":       true,
	"--version":        true,
	"--gameDir":        true,
	"--assetsDir":      true,
	"--assetIndex":     true,
	"--uuid":           true,
	"--accessToken":    true,
	"--userProperties": true,
	"--clientId":       true,
	"--xuid":           true,
	"--userType":       true,
	"--versionType":    true,
	"--width":          true,
	"--height":         true,
	"--demo":           true,
}

func JVM(arguments []mojang.Argument, os, arch string) []string {
	return filterJVM(mojang.Selected(arguments, os, arch))
}

func Game(arguments []mojang.Argument, os, arch string) []string {
	return filterGame(mojang.Selected(arguments, os, arch))
}

func Legacy(minecraftArguments string) []string {
	return filterGame(strings.Fields(minecraftArguments))
}

func filterJVM(values []string) []string {
	kept := make([]string, 0, len(values))
	for index := 0; index < len(values); index++ {
		value := values[index]
		if value == "-cp" || value == "-classpath" {
			index++
			continue
		}
		if suppliedByLauncher(value) || hasUnresolvedPlaceholder(value) {
			continue
		}
		kept = append(kept, value)
	}
	return trimmed(kept)
}

func filterGame(values []string) []string {
	kept := make([]string, 0, len(values))
	for index := 0; index < len(values); index++ {
		flag := values[index]
		value, paired := valueOf(values, index)
		if launcherGameFlags[flag] || hasUnresolvedPlaceholder(flag) || (paired && hasUnresolvedPlaceholder(value)) {
			if paired {
				index++
			}
			continue
		}
		kept = append(kept, flag)
		if paired {
			kept = append(kept, value)
			index++
		}
	}
	return trimmed(kept)
}

func valueOf(values []string, index int) (string, bool) {
	if !strings.HasPrefix(values[index], "-") || index+1 >= len(values) {
		return "", false
	}
	next := values[index+1]
	if strings.HasPrefix(next, "-") {
		return "", false
	}
	return next, true
}

func suppliedByLauncher(value string) bool {
	for _, known := range launcherJVMArgs {
		if strings.HasPrefix(value, known) {
			return true
		}
	}
	return false
}

func hasUnresolvedPlaceholder(value string) bool {
	rest := value
	for {
		start := strings.Index(rest, "${")
		if start < 0 {
			return false
		}
		end := strings.Index(rest[start:], "}")
		if end < 0 {
			return false
		}
		placeholder := rest[start : start+end+1]
		if !resolvable(placeholder) {
			return true
		}
		rest = rest[start+end+1:]
	}
}

func resolvable(placeholder string) bool {
	for _, known := range resolvablePlaceholders {
		if placeholder == known {
			return true
		}
	}
	return false
}

func trimmed(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	return values
}
