package authsetup

import (
	"fmt"

	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
	_ "github.com/laminara/laminara/server/internal/auth/providers"
	"github.com/laminara/laminara/server/internal/auth/redisstore"
	"github.com/laminara/laminara/server/internal/config"
)

func Build(cfg *config.AuthConfig) (*auth.Service, error) {
	provider, err := auth.BuildProvider(cfg.Provider, cfg.Config)
	if err != nil {
		return nil, err
	}
	sessions, err := buildSessions(cfg)
	if err != nil {
		return nil, err
	}
	serviceCfg := auth.DefaultConfig()
	if cfg.AccessTTL > 0 {
		serviceCfg.AccessTTL = cfg.AccessTTL.Duration()
	}
	if cfg.RefreshTTL > 0 {
		serviceCfg.RefreshTTL = cfg.RefreshTTL.Duration()
	}
	return auth.NewService(provider, sessions, serviceCfg), nil
}

func buildSessions(cfg *config.AuthConfig) (auth.SessionStore, error) {
	switch cfg.Sessions.Backend {
	case "", "memory":
		return auth.NewMemorySessionStore(), nil
	case "redis":
		ttl := auth.DefaultConfig().RefreshTTL
		if cfg.RefreshTTL > 0 {
			ttl = cfg.RefreshTTL.Duration()
		}
		client := redis.NewClient(&redis.Options{Addr: cfg.Sessions.RedisAddr})
		return redisstore.NewSessionStore(client, ttl), nil
	default:
		return nil, fmt.Errorf("unknown session backend %q", cfg.Sessions.Backend)
	}
}
