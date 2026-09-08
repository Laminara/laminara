package api

import (
	"net/http"
	"net/url"
	"strings"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/platform"
	"github.com/laminara/laminara/server/internal/storage"
)

const downloadPrefix = "/launcher"

func (s *Service) DownloadHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release, ok := s.currentRelease()
		if !ok {
			http.Error(w, "лаунчер ещё не опубликован", http.StatusNotFound)
			return
		}
		requested := strings.Trim(strings.TrimPrefix(r.URL.Path, downloadPrefix), "/")
		if requested == "" {
			target, ok := platformOfRequest(r)
			if !ok {
				writePlatformList(w, release)
				return
			}
			key, _ := platform.Key(target)
			http.Redirect(w, r, downloadPrefix+"/"+key, http.StatusFound)
			return
		}
		target, ok := platform.Parse(requested)
		if !ok {
			http.Error(w, "неизвестная платформа", http.StatusNotFound)
			return
		}
		artifact := artifactFor(release, target)
		if artifact == nil || artifact.Object == nil || artifact.Object.Hash == nil {
			http.Error(w, "для этой платформы лаунчер не опубликован", http.StatusNotFound)
			return
		}
		key := storage.ObjectKey(artifact.Object.Hash.Algo, artifact.Object.Hash.Value)
		location := "/objects/" + key
		if artifact.FileName != "" {
			location += "?filename=" + url.QueryEscape(artifact.FileName)
		}
		http.Redirect(w, r, location, http.StatusFound)
	})
}

func (s *Service) currentRelease() (*corev1.LauncherRelease, bool) {
	if s.releases == nil {
		return nil, false
	}
	canonical, _, err := s.releases.Current()
	if err != nil || len(canonical) == 0 {
		return nil, false
	}
	var release corev1.LauncherRelease
	if err := proto.Unmarshal(canonical, &release); err != nil {
		return nil, false
	}
	return &release, true
}

func artifactFor(release *corev1.LauncherRelease, target corev1.Platform) *corev1.LauncherArtifact {
	var fallback *corev1.LauncherArtifact
	for _, artifact := range release.Artifacts {
		if artifact.Platform != target {
			continue
		}
		if artifact.Kind == corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE {
			return artifact
		}
		if fallback == nil {
			fallback = artifact
		}
	}
	return fallback
}

func platformOfRequest(r *http.Request) (corev1.Platform, bool) {
	agent := strings.ToLower(r.UserAgent())
	switch {
	case strings.Contains(agent, "windows"):
		return platform.Parse("windows-x64")
	case strings.Contains(agent, "mac os") || strings.Contains(agent, "macintosh"):
		if strings.Contains(agent, "arm") {
			return platform.Parse("mac-os-arm64")
		}
		return platform.Parse("mac-os")
	case strings.Contains(agent, "linux") && !strings.Contains(agent, "android"):
		return platform.Parse("linux")
	}
	return corev1.Platform_PLATFORM_UNSPECIFIED, false
}

func writePlatformList(w http.ResponseWriter, release *corev1.LauncherRelease) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("Лаунчер " + release.Version + ", доступные системы:\n"))
	for _, artifact := range release.Artifacts {
		key, ok := platform.Key(artifact.Platform)
		if !ok {
			continue
		}
		_, _ = w.Write([]byte("  " + downloadPrefix + "/" + key + "\n"))
	}
}
