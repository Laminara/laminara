package access

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

func init() {
	RegisterSource("file", newFileSource)
}

type fileSourceConfig struct {
	Path string `json:"path"`
}

type fileSource struct {
	path string

	mu      sync.Mutex
	roster  *Roster
	stamp   string
	checked time.Time
}

const fileStatInterval = 2 * time.Second

func newFileSource(config json.RawMessage) (Source, error) {
	var cfg fileSourceConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, err
		}
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("file access source needs a path")
	}
	return &fileSource{path: cfg.Path}, nil
}

func (f *fileSource) load() (*Roster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	now := time.Now()
	if f.roster != nil && now.Sub(f.checked) < fileStatInterval {
		return f.roster, nil
	}
	f.checked = now

	info, err := os.Stat(f.path)
	if err != nil {
		if os.IsNotExist(err) && f.roster != nil {
			return f.roster, nil
		}
		return nil, err
	}
	stamp := fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
	if f.roster != nil && stamp == f.stamp {
		return f.roster, nil
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		return nil, err
	}
	roster, err := ParseRoster(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", f.path, err)
	}
	f.roster, f.stamp = roster, stamp
	return roster, nil
}

func (f *fileSource) Allows(_ context.Context, build string, subject Subject) (bool, error) {
	roster, err := f.load()
	if err != nil {
		return false, err
	}
	return roster.Contains(build, subject), nil
}

func (f *fileSource) Reload() {
	f.mu.Lock()
	f.roster, f.stamp, f.checked = nil, "", time.Time{}
	f.mu.Unlock()
}
