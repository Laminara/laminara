package forgeinstaller

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/laminara/laminara/server/internal/maven"
	"github.com/laminara/laminara/server/internal/progress"
)

type legacyLibrary struct {
	Name      string `json:"name"`
	URL       string `json:"url"`
	ClientReq *bool  `json:"clientreq"`
}

type legacyProfile struct {
	Install struct {
		Path      string `json:"path"`
		FilePath  string `json:"filePath"`
		Minecraft string `json:"minecraft"`
	} `json:"install"`
	VersionInfo struct {
		ID                 string          `json:"id"`
		MainClass          string          `json:"mainClass"`
		MinecraftArguments string          `json:"minecraftArguments"`
		Libraries          []legacyLibrary `json:"libraries"`
	} `json:"versionInfo"`
}

var mavenMirrors = []string{
	"https://maven.minecraftforge.net/",
	"https://libraries.minecraft.net/",
	"https://repo1.maven.org/maven2/",
}

var deadHosts = map[string]string{
	"http://files.minecraftforge.net/maven/":     "https://maven.minecraftforge.net/",
	"https://files.minecraftforge.net/maven/":    "https://maven.minecraftforge.net/",
	"http://repo.maven.apache.org/maven2/":       "https://repo1.maven.org/maven2/",
	"https://repo.maven.apache.org/maven2/":      "https://repo1.maven.org/maven2/",
	"http://libraries.minecraft.net/":            "https://libraries.minecraft.net/",
	"http://maven.minecraftforge.net/":           "https://maven.minecraftforge.net/",
	"http://repo1.maven.org/maven2/":             "https://repo1.maven.org/maven2/",
	"http://central.maven.org/maven2/":           "https://repo1.maven.org/maven2/",
	"https://central.maven.org/maven2/":          "https://repo1.maven.org/maven2/",
	"http://home.bothogames.com/mods/libraries/": "https://maven.minecraftforge.net/",
}

func (i *Installer) legacyInstall(ctx context.Context, req Request) (*LaunchInfo, error) {
	profile := i.legacy
	launch := &LaunchInfo{MainClass: profile.VersionInfo.MainClass}

	forgePath, err := maven.Path(profile.Install.Path)
	if err != nil {
		return nil, err
	}
	if err := i.extractEmbedded(profile.Install.FilePath, filepath.Join(req.LibrariesDir, filepath.FromSlash(forgePath))); err != nil {
		return nil, err
	}
	launch.Libraries = append(launch.Libraries, forgePath)

	progress.Phase(ctx, "Библиотеки загрузчика")
	for _, library := range profile.VersionInfo.Libraries {
		if library.ClientReq != nil && !*library.ClientReq {
			continue
		}
		if library.Name == profile.Install.Path {
			continue
		}
		path, err := maven.Path(library.Name)
		if err != nil {
			return nil, err
		}
		dest := filepath.Join(req.LibrariesDir, filepath.FromSlash(path))
		if err := fetchFromMirrors(ctx, req, library.URL, path, dest); err != nil {
			return nil, err
		}
		launch.Libraries = append(launch.Libraries, path)
	}

	launch.GameArgs = tweakArgs(profile.VersionInfo.MinecraftArguments)
	launch.ClientJar = ""
	return launch, nil
}

func fetchFromMirrors(ctx context.Context, req Request, declared, path, dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}

	var lastErr error
	for _, base := range mirrorsFor(declared) {
		if err := req.Download(ctx, base+path, dest, ""); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return fmt.Errorf("библиотеку %s не отдал ни один репозиторий: %w", path, lastErr)
}

func mirrorsFor(declared string) []string {
	bases := make([]string, 0, len(mavenMirrors)+1)
	if declared != "" {
		if replacement, dead := deadHosts[declared]; dead {
			declared = replacement
		}
		if !strings.HasSuffix(declared, "/") {
			declared += "/"
		}
		bases = append(bases, declared)
	}
	for _, mirror := range mavenMirrors {
		if !contains(bases, mirror) {
			bases = append(bases, mirror)
		}
	}
	return bases
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func tweakArgs(arguments string) []string {
	fields := strings.Fields(arguments)
	var extra []string
	for index := 0; index < len(fields); index++ {
		if fields[index] != "--tweakClass" || index+1 >= len(fields) {
			continue
		}
		extra = append(extra, "--tweakClass", fields[index+1])
		index++
	}
	return extra
}

func (i *Installer) extractEmbedded(name, dest string) error {
	if name == "" {
		return fmt.Errorf("в установщике не указан файл загрузчика")
	}
	reader, err := zip.OpenReader(i.jarPath)
	if err != nil {
		return err
	}
	defer reader.Close()

	wanted := strings.TrimPrefix(name, "/")
	for _, file := range reader.File {
		if file.Name != wanted {
			continue
		}
		source, err := file.Open()
		if err != nil {
			return err
		}
		defer source.Close()

		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		target, err := os.Create(dest)
		if err != nil {
			return err
		}
		defer target.Close()
		_, err = io.Copy(target, source)
		return err
	}
	return fmt.Errorf("файла %s нет в установщике", wanted)
}
