package skin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Textures struct {
	SkinURL string
	CapeURL string
	Slim    bool
}

type Provider interface {
	Textures(ctx context.Context, username, uuid string) (Textures, error)
}

type ProviderFactory func(config json.RawMessage) (Provider, error)

var factories = map[string]ProviderFactory{}

func Register(name string, factory ProviderFactory) {
	factories[name] = factory
}

func Build(name string, config json.RawMessage) (Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("unknown skin provider %q", name)
	}
	return factory(config)
}

func substitute(template, username, uuid string) string {
	return strings.NewReplacer(
		"%nickname%", username,
		"%username%", username,
		"%uuid%", uuid,
	).Replace(template)
}
