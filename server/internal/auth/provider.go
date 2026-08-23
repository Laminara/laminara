package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInvalidToken       = errors.New("invalid token")
)

type Provider interface {
	Authenticate(ctx context.Context, creds Credentials) (Identity, error)
}

type ProviderFactory func(config json.RawMessage) (Provider, error)

var providerFactories = map[string]ProviderFactory{}

func RegisterProvider(name string, factory ProviderFactory) {
	providerFactories[name] = factory
}

func BuildProvider(name string, config json.RawMessage) (Provider, error) {
	factory, ok := providerFactories[name]
	if !ok {
		return nil, fmt.Errorf("unknown auth provider %q", name)
	}
	return factory(config)
}
