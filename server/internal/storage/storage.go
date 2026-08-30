package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"time"
)

type LocationKind int

const (
	LocationInternal LocationKind = iota
	LocationURL
)

type Location struct {
	Kind         LocationKind
	URL          string
	InternalPath string
}

type Backend interface {
	Put(ctx context.Context, key string, r io.Reader, size int64) error
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Stat(ctx context.Context, key string) (size int64, exists bool, err error)
	Delete(ctx context.Context, key string) error
	Locate(ctx context.Context, key string, ttl time.Duration) (Location, error)
}

type BackendFactory func(config json.RawMessage) (Backend, error)

var backendFactories = map[string]BackendFactory{}

func RegisterBackend(name string, factory BackendFactory) {
	backendFactories[name] = factory
}

func BackendNames() []string {
	names := make([]string, 0, len(backendFactories))
	for name := range backendFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func BuildBackend(name string, config json.RawMessage) (Backend, error) {
	factory, ok := backendFactories[name]
	if !ok {
		return nil, fmt.Errorf("unknown storage backend %q", name)
	}
	return factory(config)
}
