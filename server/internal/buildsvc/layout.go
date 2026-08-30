package buildsvc

import (
	"os"
	"path/filepath"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/platform"
)

type buildLayout struct {
	root      string
	flat      bool
	platforms []corev1.Platform
}

func (s *Service) layout(name string) buildLayout {
	root := filepath.Join(s.profilesDir, name)
	layout := buildLayout{root: root}
	if _, err := os.Stat(filepath.Join(root, manifest.LaunchProfileName)); err == nil {
		layout.flat = true
		return layout
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return layout
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		p, ok := platform.Parse(entry.Name())
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(root, entry.Name(), manifest.LaunchProfileName)); err != nil {
			continue
		}
		layout.platforms = append(layout.platforms, p)
	}
	return layout
}

func (l buildLayout) exists() bool {
	return l.flat || len(l.platforms) > 0
}

func (l buildLayout) dir(p corev1.Platform) (string, string) {
	if l.flat {
		return l.root, l.root
	}
	key, _ := platform.Key(p)
	return filepath.Join(l.root, key), l.root
}
