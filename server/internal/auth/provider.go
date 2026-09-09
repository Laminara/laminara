package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrInvalidCredentials = errors.New("неверный логин или пароль")
	ErrInvalidToken       = errors.New("токен недействителен")
	ErrTwoFactorRequired  = errors.New("нужен код из приложения-аутентификатора")

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
		return nil, fmt.Errorf("источника аккаунтов «%s» нет — выберите из: %s", name, strings.Join(ProviderNames(), ", "))
	}
	if len(config) == 0 {
		config = json.RawMessage("{}")
	}
	return factory(config)
}
