package config

import (
	"errors"
	"strings"
	"testing"
)

func TestPlaceholderInTrustedProxiesIsRefused(t *testing.T) {
	cfg := &Config{API: &APIConfig{Addr: "0.0.0.0:8099", TrustedProxies: []string{"не задано"}}}

	err := Validate(cfg)
	if err == nil {
		t.Fatal("подпись «не задано» попала бы в список адресов и уронила сервер в цикл перезапусков")
	}
	var broken *BadConfigError
	if !errors.As(err, &broken) {
		t.Fatalf("ошибка должна помечаться как ошибка настройки, получили %T", err)
	}
	if broken.Setting != "api.trustedProxies" {
		t.Fatalf("не названа настройка: %+v", broken)
	}
	if !strings.Contains(err.Error(), "уберите его целиком") {
		t.Fatalf("нет подсказки, что делать: %v", err)
	}
}

func TestGoodConfigPasses(t *testing.T) {
	cfg := &Config{
		API:  &APIConfig{Addr: "127.0.0.1:8099", TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8", " "}},
		Auth: &AuthConfig{Provider: "sql"},
	}
	if err := Validate(cfg); err != nil {
		t.Fatalf("рабочие настройки отвергнуты: %v", err)
	}
}

func TestBadListenAddressIsRefused(t *testing.T) {
	if err := Validate(&Config{API: &APIConfig{Addr: "почему-то-не-адрес"}}); err == nil {
		t.Fatal("адрес без порта должен отвергаться до старта")
	}
}

func TestEmptyProviderIsRefused(t *testing.T) {
	if err := Validate(&Config{Auth: &AuthConfig{}}); err == nil {
		t.Fatal("пустой источник аккаунтов должен отвергаться — войти не сможет никто")
	}
}
