package config

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

const ExitBadConfig = 78

type BadConfigError struct {
	Setting string
	Reason  string
}

func (e *BadConfigError) Error() string {
	return fmt.Sprintf("%s: %s", e.Setting, e.Reason)
}

func badConfig(setting, format string, args ...any) error {
	return &BadConfigError{Setting: setting, Reason: fmt.Sprintf(format, args...)}
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if err := validateAPI(cfg.API); err != nil {
		return err
	}
	if cfg.Auth != nil && strings.TrimSpace(cfg.Auth.Provider) == "" {
		return badConfig("auth.provider", "не указан источник аккаунтов")
	}
	return nil
}

func validateAPI(api *APIConfig) error {
	if api == nil {
		return nil
	}
	if addr := strings.TrimSpace(api.Addr); addr != "" {
		if _, _, err := net.SplitHostPort(addr); err != nil {
			return badConfig("api.addr", "%q не адрес вида хост:порт", api.Addr)
		}
	}
	for _, raw := range api.TrustedProxies {
		text := strings.TrimSpace(raw)
		if text == "" {
			continue
		}
		if strings.Contains(text, "/") {
			if _, err := netip.ParsePrefix(text); err != nil {
				return badConfig("api.trustedProxies", "%q не подсеть вида 10.0.0.0/8", raw)
			}
			continue
		}
		if _, err := netip.ParseAddr(text); err != nil {
			return badConfig("api.trustedProxies", "%q не адрес и не подсеть; чтобы очистить список, уберите его целиком", raw)
		}
	}
	return nil
}
