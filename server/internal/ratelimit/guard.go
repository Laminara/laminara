package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/duration"
	"github.com/laminara/laminara/server/internal/redisconf"
)

type Guard struct {
	login     Limiter
	account   Limiter
	challenge Limiter
}

type Bucket struct {
	Limit int      `json:"limit"`
	Per   Duration `json:"per"`
}

type Config struct {
	Disabled bool             `json:"disabled"`
	Backend  string           `json:"backend"`
	Redis    redisconf.Config `json:"redis"`

	Login     Bucket `json:"login"`
	Account   Bucket `json:"account"`
	Challenge Bucket `json:"challenge"`
}

func (c *Config) UnmarshalJSON(data []byte) error {
	type plain Config
	var raw struct {
		plain
		RedisAddr string `json:"redisAddr"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = Config(raw.plain)
	c.Redis = c.Redis.WithFallbackAddr(raw.RedisAddr)
	return nil
}

type Duration = duration.Duration

func (b Bucket) orElse(limit int, per time.Duration) Bucket {
	if b.Limit <= 0 {
		b.Limit = limit
	}
	if b.Per <= 0 {
		b.Per = Duration(per)
	}
	return b
}

func New(cfg *Config) (*Guard, error) {
	resolved := Config{}
	if cfg != nil {
		resolved = *cfg
	}
	if resolved.Disabled {
		return nil, nil
	}

	resolved.Login = resolved.Login.orElse(10, 5*time.Minute)
	resolved.Account = resolved.Account.orElse(50, 15*time.Minute)
	resolved.Challenge = resolved.Challenge.orElse(60, time.Minute)

	build := func(bucket Bucket) Limiter { return NewMemoryLimiter(bucket.Limit, bucket.Per.Duration()) }
	switch strings.ToLower(resolved.Backend) {
	case "", "memory":
	case "redis":
		if !resolved.Redis.Set() {
			return nil, fmt.Errorf("rateLimit.backend is redis but rateLimit.redis.addr is empty")
		}
		client := resolved.Redis.Client()
		build = func(bucket Bucket) Limiter { return NewRedisLimiter(client, bucket.Limit, bucket.Per.Duration()) }
	default:
		return nil, fmt.Errorf("unknown rateLimit.backend %q (want memory or redis)", resolved.Backend)
	}

	return &Guard{
		login:     build(resolved.Login),
		account:   build(resolved.Account),
		challenge: build(resolved.Challenge),
	}, nil
}

func (g *Guard) SignInAllowed(ctx context.Context, address, username string) bool {
	if g == nil {
		return true
	}
	for limiter, key := range map[Limiter]string{g.login: "login:" + address, g.account: "account:" + fold(username)} {
		if blocked, err := limiter.Blocked(ctx, key); err != nil || blocked {
			return false
		}
	}
	return true
}

func (g *Guard) SignInFailed(ctx context.Context, address, username string) {
	if g == nil {
		return
	}
	_, _ = g.login.Allow(ctx, "login:"+address)
	_, _ = g.account.Allow(ctx, "account:"+fold(username))
}

func (g *Guard) ChallengeAllowed(ctx context.Context, address string) bool {
	if g == nil {
		return true
	}
	allowed, err := g.challenge.Allow(ctx, "challenge:"+address)
	return err == nil && allowed
}

func fold(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
