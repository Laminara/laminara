package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
	ErrTwoFactorRequired  = errors.New("two-factor code required")

	ErrSourceUnavailable   = errors.New("источник аккаунтов недоступен")
	ErrSessionsUnavailable = errors.New("хранилище сессий недоступно")
)

type Provider interface {
	Authenticate(ctx context.Context, creds Credentials) (Identity, error)
}

type ProviderFactory func(config json.RawMessage) (Provider, error)

var providerFactories = map[string]ProviderFactory{}

func RegisterProvider(name string, factory ProviderFactory) {
	providerFactories[name] = factory
}

func ProviderNames() []string {
	names := make([]string, 0, len(providerFactories))
	for name := range providerFactories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func BuildProvider(name string, config json.RawMessage) (Provider, error) {
	factory, ok := providerFactories[name]
	if !ok {
		return nil, fmt.Errorf("unknown auth provider %q", name)
	}
	return factory(config)
}
