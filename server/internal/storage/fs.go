package storage

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"time"
)

func init() {
	RegisterBackend("fs", newFS)
}

var _ Backend = (*fsBackend)(nil)

type fsConfig struct {
	Root         string `json:"root"`
	XAccelPrefix string `json:"xaccelPrefix"`
}

type fsBackend struct {
	root         string
	xaccelPrefix string
}

func newFS(raw json.RawMessage) (Backend, error) {
	var cfg fsConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Root == "" {
		return nil, errors.New("fs storage requires a root directory")
	}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return nil, err
	}
	prefix := cfg.XAccelPrefix
	if prefix == "" {
		prefix = "/_objects"
	}
	return &fsBackend{root: cfg.Root, xaccelPrefix: prefix}, nil
}

func (b *fsBackend) resolve(key string) string {
	return filepath.Join(b.root, filepath.FromSlash(key))
}

func (b *fsBackend) Put(_ context.Context, key string, r io.Reader, _ int64) error {
	full := b.resolve(key)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(full), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, r); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), full)
}

func (b *fsBackend) Get(_ context.Context, key string) (io.ReadCloser, error) {
	return os.Open(b.resolve(key))
}

func (b *fsBackend) Stat(_ context.Context, key string) (int64, bool, error) {
	info, err := os.Stat(b.resolve(key))
	if errors.Is(err, os.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return info.Size(), true, nil
}

func (b *fsBackend) Delete(_ context.Context, key string) error {
	if err := os.Remove(b.resolve(key)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func (b *fsBackend) List(_ context.Context, prefix string) ([]string, error) {
	base := b.resolve(prefix)
	var keys []string
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(b.root, p)
		if err != nil {
			return err
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	return keys, err
}

func (b *fsBackend) Locate(_ context.Context, key string, _ time.Duration) (Location, error) {
	return Location{Kind: LocationInternal, InternalPath: path.Join(b.xaccelPrefix, key)}, nil
}
