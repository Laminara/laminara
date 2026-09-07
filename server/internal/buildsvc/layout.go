package buildsvc

import (
	"os"
	"path/filepath"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/platform"
)

const platformsDir = "platforms"

type buildLayout struct {
	root      string
	flat      bool
	shared    bool
	platforms []corev1.Platform
}

type placement struct {
	shared   string
	platform string
}

func (s *Service) layout(name string) buildLayout {
	root := filepath.Join(s.profilesDir, name)
	layout := buildLayout{root: root}
	if _, err := os.Stat(filepath.Join(root, manifest.LaunchProfileName)); err == nil {
		layout.flat = true
		return layout
	}
	if found := platformsUnder(filepath.Join(root, platformsDir)); len(found) > 0 {
		layout.shared = true
		layout.platforms = found
		return layout
	}
	layout.platforms = platformsUnder(root)
	return layout
}

func platformsUnder(dir string) []corev1.Platform {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var found []corev1.Platform
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p, ok := platform.Parse(entry.Name())
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, entry.Name(), manifest.LaunchProfileName)); err != nil {
			continue
		}
		found = append(found, p)
	}
	return found
}

func (l buildLayout) exists() bool {
	return l.flat || len(l.platforms) > 0
}

func (l buildLayout) place(p corev1.Platform) placement {
	if l.flat {
		return placement{shared: l.root, platform: l.root}
	}
	key, _ := platform.Key(p)
	if l.shared {
		return placement{shared: l.root, platform: filepath.Join(l.root, platformsDir, key)}
	}
	return placement{shared: filepath.Join(l.root, key), platform: filepath.Join(l.root, key)}
}

func (l buildLayout) dir(p corev1.Platform) (string, string) {
	placed := l.place(p)
	return placed.platform, l.root
}

func sharedPlacement(root string, p corev1.Platform) placement {
	key, _ := platform.Key(p)
	return placement{shared: root, platform: filepath.Join(root, platformsDir, key)}
}
