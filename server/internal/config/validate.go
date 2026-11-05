package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"strings"

	"github.com/laminara/laminara/server/internal/access"
)

const ExitBadConfig = 78

type BadConfigError struct {
	Setting string
	Reason  string
	Err     error
}

func (e *BadConfigError) Error() string {
	if e.Setting == "" {
		return e.Reason
	}
	return fmt.Sprintf("%s: %s", e.Setting, e.Reason)
}

func (e *BadConfigError) Unwrap() error { return e.Err }

func Fatal(err error) error {
	if err == nil {
		return nil
	}
	var broken *BadConfigError
	if errors.As(err, &broken) {
		return err
	}
	return &BadConfigError{Reason: err.Error(), Err: err}
}

func badConfig(setting, format string, args ...any) error {
	return &BadConfigError{Setting: setting, Reason: fmt.Sprintf(format, args...)}
}

func Validate(cfg *Config) error {
	if cfg == nil {
		return nil
	}
	if cfg.Launcher != nil && (cfg.Launcher.MaxAccounts < 0 || cfg.Launcher.MaxAccounts > 20) {
		return badConfig("launcher.maxAccounts", "допустимо от 1 до 20; 0 использует значение по умолчанию 5")
	}
	if err := access.ValidateRules(cfg.Access); err != nil {
		return badConfig("access.rules", "%s", err)
	}
	if err := validateAPI(cfg.API); err != nil {
		return err
	}
	if err := validateAdmin(cfg.Admin); err != nil {
		return err
	}
	if cfg.Log != nil {
		if cfg.Log.Format != "" && cfg.Log.Format != "text" && cfg.Log.Format != "json" {
			return badConfig("log.format", "выберите text или json")
		}
		var level slog.Level
		if cfg.Log.Level != "" {
			if err := level.UnmarshalText([]byte(cfg.Log.Level)); err != nil {
				return badConfig("log.level", "выберите debug, info, warn или error")
			}
		}
	}
	if cfg.Auth != nil && strings.TrimSpace(cfg.Auth.Provider) == "" {
		return badConfig("auth.provider", "не указан источник аккаунтов")
	}
	return validateBackends(cfg)
}

func validateAdmin(admin *AdminConfig) error {
	if admin == nil || admin.Addr == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(admin.Addr)
	if err != nil {
		return badConfig("admin.addr", "нужен адрес вида хост:порт")
	}
	if (admin.TLSCert == "") != (admin.TLSKey == "") {
		return badConfig("admin", "tlsCert и tlsKey задаются вместе")
	}
	if admin.TLSCert == "" {
		ip := net.ParseIP(host)
		if !admin.AllowInsecureLoopback || ip == nil || !ip.IsLoopback() {
			return badConfig("admin", "для сетевого доступа нужны tlsCert и tlsKey; HTTP допускается только на loopback с allowInsecureLoopback")
		}
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
