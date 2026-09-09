package skin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
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

func ProviderNames() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func Build(name string, config json.RawMessage) (Provider, error) {
	factory, ok := factories[name]
	if !ok {
		return nil, fmt.Errorf("источника скинов «%s» нет — выберите из: %s", name, strings.Join(ProviderNames(), ", "))
	}
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	return factory(config)
}

func substitute(template, username, uuid string) string {
	safeName := url.PathEscape(username)
	safeUUID := url.PathEscape(uuid)
	return strings.NewReplacer(
		"%nickname%", safeName,
		"%username%", safeName,
		"%uuid%", safeUUID,
		"%hash%", url.PathEscape(strings.ReplaceAll(uuid, "-", "")),
	).Replace(template)
}
