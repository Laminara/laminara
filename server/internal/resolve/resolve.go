package resolve

import (
	"fmt"
	"strings"

	"github.com/laminara/laminara/server/internal/launchargs"
	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/maven"
	"github.com/laminara/laminara/server/internal/mojang"
)

type Artifact struct {
	Path string
	SHA1 string
	Size int64
	URL  string
}

type Profile struct {
	MainClass     string
	JavaComponent string
	JavaMajor     int
	ClientJar     Artifact
	Libraries     []Artifact
	Natives       []Artifact
	JvmArgs       []string
	GameArgs      []string
	AssetIndexID  string
	AssetIndexURL string
}

const defaultLoaderMaven = "https://maven.fabricmc.net/"

func Resolve(detail *mojang.VersionDetail, os, arch string, loaderProfile *loader.LoaderProfile) (*Profile, error) {
	profile := &Profile{
		MainClass:     detail.MainClass,
		JavaComponent: detail.JavaVersion.Component,
		JavaMajor:     detail.JavaVersion.MajorVersion,
		ClientJar: Artifact{
			Path: fmt.Sprintf("versions/%s/%s.jar", detail.ID, detail.ID),
			SHA1: detail.Downloads.Client.SHA1,
			Size: detail.Downloads.Client.Size,
			URL:  detail.Downloads.Client.URL,
		},
		JvmArgs:       launchargs.JVM(detail.Arguments.JVM, os, arch),
		GameArgs:      gameArgs(detail, os, arch),
		AssetIndexID:  detail.AssetIndex.ID,
		AssetIndexURL: detail.AssetIndex.URL,
	}

	for _, lib := range detail.Libraries {
		if !mojang.EvaluateRules(lib.Rules, os, arch) {
			continue
		}
		if classifierKey, ok := lib.Natives[os]; ok {
			classifierKey = strings.ReplaceAll(classifierKey, "${arch}", archBits(arch))
			if declared := lib.Downloads.Classifiers[classifierKey]; declared != nil {
				artifact, err := fromArtifact(*declared, lib.Name+":"+classifierKey)
				if err != nil {
					return nil, err
				}
				profile.Natives = append(profile.Natives, artifact)
			}
			continue
		}
		if lib.Downloads.Artifact != nil {
			artifact, err := fromArtifact(*lib.Downloads.Artifact, lib.Name)
			if err != nil {
				return nil, err
			}
			profile.Libraries = append(profile.Libraries, artifact)
			continue
		}
		if lib.URL != "" {
			artifact, err := mavenArtifact(lib.Name, lib.URL)
			if err != nil {
				return nil, err
			}
			profile.Libraries = append(profile.Libraries, artifact)
		}
	}

	if loaderProfile != nil {
		if loaderProfile.MainClass != "" {
			profile.MainClass = loaderProfile.MainClass
		}
		for _, lib := range loaderProfile.Libraries {
			base := lib.URL
			if base == "" {
				base = defaultLoaderMaven
			}
			artifact, err := mavenArtifact(lib.Name, base)
			if err != nil {
				return nil, err
			}
			profile.Libraries = append(profile.Libraries, artifact)
		}
	}
	return profile, nil
}

func gameArgs(detail *mojang.VersionDetail, os, arch string) []string {
	if len(detail.Arguments.Game) > 0 {
		return launchargs.Game(detail.Arguments.Game, os, arch)
	}
	return launchargs.Legacy(detail.MinecraftArguments)
}

func fromArtifact(a mojang.Artifact, name string) (Artifact, error) {
	path := a.Path
	if path == "" {
		derived, err := maven.Path(name)
		if err != nil {
			return Artifact{}, fmt.Errorf("библиотека «%s» не говорит, куда её класть, и координаты не разобрать: %w", name, err)
		}
		path = derived
	}
	return Artifact{Path: "libraries/" + path, SHA1: a.SHA1, Size: a.Size, URL: a.URL}, nil
}

func mavenArtifact(coords, base string) (Artifact, error) {
	path, err := maven.Path(coords)
	if err != nil {
		return Artifact{}, err
	}
	return Artifact{Path: "libraries/" + path, URL: strings.TrimRight(base, "/") + "/" + path}, nil
}

func archBits(arch string) string {
	if arch == "x86" {
		return "32"
	}
	return "64"
}
