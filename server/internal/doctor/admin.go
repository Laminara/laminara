package doctor

import (
	"context"
	"crypto/tls"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/controlauth"
	"github.com/laminara/laminara/server/internal/diag"
	"time"
)

func checkAdmin(_ context.Context, opts Options, probe *diag.Probe) {
	if opts.Config == nil || opts.Config.Admin == nil || opts.Config.Admin.Addr == "" {
		probe.Skip("сетевое управление", "выключено; доступен локальный сокет")
		return
	}
	cfg := opts.Config.Admin
	if err := config.Validate(&config.Config{Admin: cfg}); err != nil {
		probe.Fail("сетевое управление", err.Error(), diag.Remedy{})
		return
	}
	if cfg.TLSCert != "" {
		if _, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey); err != nil {
			probe.Fail("TLS", "не удалось прочитать пару сертификат/ключ", diag.Remedy{})
			return
		}
		probe.OK("TLS", "сертификат и ключ читаются")
	}
	tokens, err := controlauth.NewStore(cfg.TokensPath).List()
	if err != nil {
		probe.Fail("токены", "хранилище токенов не читается", diag.Remedy{})
		return
	}
	active := 0
	for _, token := range tokens {
		if !token.Revoked && time.Now().Before(token.Expires) {
			active++
		}
	}
	if active == 0 {
		probe.Warn("токены", "действующих токенов нет", diag.Remedy{Hint: "выдайте токен локально через exec admin-token create"})
		return
	}
	probe.OK("токены", "действующих токенов: %d", active)
}
