package serversetup

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/api"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/authsetup"
	"github.com/laminara/laminara/server/internal/buildsvc"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/clientaddr"
	"github.com/laminara/laminara/server/internal/clientconfig"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/crash"
	"github.com/laminara/laminara/server/internal/events"
	"github.com/laminara/laminara/server/internal/health"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/launchersvc"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/ratelimit"
	"github.com/laminara/laminara/server/internal/selfupdate"
	"github.com/laminara/laminara/server/internal/signing"
	"github.com/laminara/laminara/server/internal/skin"
	"github.com/laminara/laminara/server/internal/storage"
	"github.com/laminara/laminara/server/internal/version"
	"github.com/laminara/laminara/server/internal/webconsole"
	"github.com/laminara/laminara/server/internal/yggdrasil"
)

type Wired struct {
	Launcher      *launchersvc.Service
	Auth          *auth.Service
	Build         *buildsvc.Service
	Catalog       *catalog.Catalog
	Access        *access.Controller
	Machines      *hwid.Gate
	Signing       *signing.Keyring
	Limits        *ratelimit.Guard
	News          *news.Service
	Crashes       *crash.Service
	Console       *webconsole.Service
	PublicHandler http.Handler
	PublicAddr    string
	Events        *events.Bus
}

const healthProbeKey = "healthz/probe"

func Build(cfg *config.Config) (*Wired, error) {
	wired := &Wired{Events: events.NewBus()}

	if cfg.Auth != nil {
		authService, err := authsetup.Build(cfg.Auth)
		if err != nil {
			return nil, err
		}
		wired.Auth = authService
	}

	var backend storage.Backend
	if cfg.Storage != nil {
		b, err := storage.BuildBackend(cfg.Storage.Backend, cfg.Storage.Config)
		if err != nil {
			return nil, err
		}
		backend = b
	}

	if backend != nil && cfg.Build != nil && cfg.Build.ProfilesDir != "" {
		if cfg.Build.SigningKeyPath == "" {
			return nil, errors.New("в конфиге нет build.signingKeyPath — без него ключ подписи создаётся заново при каждом запуске, и старые лаунчеры перестают принимать манифесты")
		}
		ring, err := signing.NewKeyring(cfg.Build.SigningKeyPath, cfg.Build.TrustedSigningKeys)
		if err != nil {
			return nil, err
		}
		wired.Signing = ring
		key := ring.Active()
		cas := storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3)
		wired.Build = buildsvc.NewService(cas, manifest.NewSigner(key), cfg.Build.ProfilesDir)
		if cfg.Launcher != nil && cfg.Launcher.Dir != "" {
			wired.Launcher = launchersvc.NewService(cas, manifest.NewSigner(key), cfg.Launcher.Dir)
			launcherBus := wired.Events
			wired.Launcher.SetEmitter(func(topic string, data map[string]string) {
				launcherBus.Publish(events.Event{Topic: topic, Data: data})
			})
			wired.Launcher.SetBakery(&launchersvc.Bakery{
				Repo:    cfg.Update.RepoOr(selfupdate.DefaultRepo),
				Version: version.Current,
				Auto:    cfg.Launcher.Bakes(),
				Document: func() (clientconfig.Document, error) {
					return clientconfig.Build(cfg, cfg.Launcher.Endpoints)
				},
			})
		}
		bus := wired.Events
		wired.Build.SetEmitter(func(topic string, data map[string]string) {
			bus.Publish(events.Event{Topic: topic, Data: data})
		})
	}

	controller, err := access.New(cfg.Access)
	if err != nil {
		return nil, err
	}
	wired.Access = controller

	machines, err := hwid.New(cfg.HWID)
	if err != nil {
		return nil, err
	}
	wired.Machines = machines

	limits, err := ratelimit.New(cfg.RateLimit)
	if err != nil {
		return nil, err
	}
	wired.Limits = limits

	announcements, err := news.New(cfg.News)
	if err != nil {
		return nil, err
	}
	wired.News = announcements

	crashes, err := crash.New(cfg.Crashes)
	if err != nil {
		return nil, err
	}
	wired.Crashes = crashes

	console, err := webconsole.New(cfg.Console, slog.Default())
	if err != nil {
		return nil, err
	}
	wired.Console = console
	if cfg.Build != nil && cfg.Build.ProfilesDir != "" {
		wired.Catalog = catalog.New(cfg.Build.ProfilesDir)
		served := wired.Catalog
		wired.Events.Subscribe(func(e events.Event) {
			if strings.HasPrefix(e.Topic, "build.") {
				served.Refresh()
			}
		})
		if wired.Build != nil {
			wired.Build.SetCatalog(served)
		}
	}

	handler, err := buildPublicHandler(cfg, wired, backend)
	if err != nil {
		return nil, err
	}
	if handler != nil && cfg.API != nil && cfg.API.Addr != "" {
		wired.PublicHandler = handler
		wired.PublicAddr = cfg.API.Addr
	}

	return wired, nil
}

func buildPublicHandler(cfg *config.Config, wired *Wired, backend storage.Backend) (http.Handler, error) {
	authService := wired.Auth
	var trustedProxies []string
	if cfg.API != nil {
		trustedProxies = cfg.API.TrustedProxies
	}
	proxies, err := clientaddr.New(trustedProxies)
	if err != nil {
		return nil, err
	}
	var launcher http.Handler
	if backend != nil && wired.Catalog != nil {
		xAccel := cfg.API != nil && cfg.API.XAccel
		var releases *launchersvc.Releases
		if cfg.Launcher != nil && cfg.Launcher.Dir != "" {
			releases = launchersvc.NewReleases(cfg.Launcher.Dir)
		}
		service := api.NewService(api.Options{
			Auth:     authService,
			Catalog:  wired.Catalog,
			Releases: releases,
			Access:   wired.Access,
			Machines: wired.Machines,
			Limits:   wired.Limits,
			News:     wired.News,
			Crashes:  wired.Crashes,
			Proxies:  proxies,
			Log:      slog.Default(),
		})
		launcher = api.Handler(service, backend, xAccel)
	}

	mux := http.NewServeMux()
	healthChecks(wired, backend).Mount(mux)
	wired.Console.Mount(mux)
	if launcher != nil {
		mux.Handle("/", launcher)
	}

	if cfg.Yggdrasil == nil || !cfg.Yggdrasil.Enabled || authService == nil {
		return mux, nil
	}

	skinProvider, err := skin.Build(cfg.Yggdrasil.SkinProvider, cfg.Yggdrasil.SkinConfig)
	if err != nil {
		return nil, err
	}
	yggServer, err := yggdrasil.NewServer(authService, skinProvider, wired.Machines, wired.Limits, yggdrasil.Config{
		ServerName:  cfg.Yggdrasil.ServerName,
		SkinDomains: cfg.Yggdrasil.SkinDomains,
		RSAKeyPath:  cfg.Yggdrasil.RSAKeyPath,
		Proxies:     proxies,
	})
	if err != nil {
		return nil, err
	}

	mux.Handle("/yggdrasil/", http.StripPrefix("/yggdrasil", yggServer.Handler()))
	return mux, nil
}

func healthChecks(wired *Wired, backend storage.Backend) *health.Handler {
	var checks []health.Check
	if backend != nil {
		checks = append(checks, health.Check{
			Name: "storage",
			Probe: func(ctx context.Context) error {
				_, _, err := backend.Stat(ctx, healthProbeKey)
				return err
			},
		})
	}
	if wired.Catalog != nil {
		served := wired.Catalog
		checks = append(checks, health.Check{
			Name: "catalog",
			Probe: func(context.Context) error {
				_, err := served.List()
				return err
			},
		})
	}
	return health.New(checks...)
}
