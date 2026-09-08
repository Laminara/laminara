package tempdir

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/laminara/laminara/server/internal/config"
)

var system = sync.OnceValue(os.TempDir)

func System() string {
	return system()
}

func Writable(dir string) error {
	if dir == "" {
		return errors.New("каталог не задан")
	}
	probe, err := os.CreateTemp(dir, ".laminara-probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	probe.Close()
	return os.Remove(name)
}

func Candidates(cfg *config.Config, configPath string) []string {
	var dirs []string
	if cfg != nil {
		if cfg.Build != nil && cfg.Build.SigningKeyPath != "" {
			dirs = append(dirs, filepath.Dir(cfg.Build.SigningKeyPath))
		}
		if cfg.Storage != nil && cfg.Storage.Backend == "fs" {
			var fs struct {
				Root string `json:"root"`
			}
			if json.Unmarshal(cfg.Storage.Config, &fs) == nil {
				dirs = append(dirs, fs.Root)
			}
		}
		if cfg.Launcher != nil {
			dirs = append(dirs, cfg.Launcher.Dir)
		}
	}
	if configPath != "" {
		dirs = append(dirs, filepath.Dir(configPath))
	}
	return dirs
}

func Fallback(candidates ...string) (string, error) {
	var failures []error
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		dir := filepath.Join(candidate, "tmp")
		if err := os.MkdirAll(dir, 0o700); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := Writable(dir); err != nil {
			failures = append(failures, err)
			continue
		}
		return dir, nil
	}
	return "", errors.Join(append([]error{errors.New("запасной временный каталог завести не вышло")}, failures...)...)
}

func Ensure(candidates ...string) (string, bool, error) {
	current := System()
	if Writable(current) == nil {
		return current, false, nil
	}
	dir, err := Fallback(candidates...)
	if err != nil {
		return current, false, err
	}
	if err := os.Setenv("TMPDIR", dir); err != nil {
		return current, false, err
	}
	return dir, true, nil
}
