package resolve

import (
	"fmt"
	"strings"

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
		AssetIndexID:  detail.AssetIndex.ID,
		AssetIndexURL: detail.AssetIndex.URL,
	}

	for _, lib := range detail.Libraries {
		if !EvaluateRules(lib.Rules, os, arch) {
			continue
		}
		if classifierKey, ok := lib.Natives[os]; ok {
			classifierKey = strings.ReplaceAll(classifierKey, "${arch}", archBits(arch))
			if artifact := lib.Downloads.Classifiers[classifierKey]; artifact != nil {
				profile.Natives = append(profile.Natives, fromArtifact(*artifact))
			}
			continue
		}
		if lib.Downloads.Artifact != nil {
			profile.Libraries = append(profile.Libraries, fromArtifact(*lib.Downloads.Artifact))
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

func EvaluateRules(rules []mojang.Rule, os, arch string) bool {
	if len(rules) == 0 {
		return true
	}
	allowed := false
	for _, rule := range rules {
		if ruleMatches(rule, os, arch) {
			allowed = rule.Action == "allow"
		}
	}
	return allowed
}

func ruleMatches(rule mojang.Rule, os, arch string) bool {
	if len(rule.Features) > 0 {
		return false
	}
	if rule.OS == nil {
		return true
	}
	if rule.OS.Name != "" && rule.OS.Name != os {
		return false
	}
	if rule.OS.Arch != "" && rule.OS.Arch != arch {
		return false
	}
	return true
}

func fromArtifact(a mojang.Artifact) Artifact {
	return Artifact{Path: "libraries/" + a.Path, SHA1: a.SHA1, Size: a.Size, URL: a.URL}
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
