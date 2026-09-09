package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
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

var (
	DefaultLogin     = Bucket{Limit: 10, Per: Duration(5 * time.Minute)}
	DefaultAccount   = Bucket{Limit: 50, Per: Duration(15 * time.Minute)}
	DefaultChallenge = Bucket{Limit: 60, Per: Duration(time.Minute)}
)

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

	resolved.Login = resolved.Login.orElse(DefaultLogin.Limit, DefaultLogin.Per.Duration())
	resolved.Account = resolved.Account.orElse(DefaultAccount.Limit, DefaultAccount.Per.Duration())
	resolved.Challenge = resolved.Challenge.orElse(DefaultChallenge.Limit, DefaultChallenge.Per.Duration())

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
	checks := []struct {
		limiter Limiter
		key     string
	}{
		{g.login, "login:" + address},
		{g.account, "account:" + fold(username)},
	}
	for _, check := range checks {
		blocked, err := check.limiter.Blocked(ctx, check.key)
		if err != nil {
			slog.Default().Error("счётчик попыток входа не отвечает — пускаю игроков дальше, чтобы вход не встал у всех",
				"source", "ratelimit",
				"ошибка", err,
			)
			continue
		}
		if blocked {
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
	if err != nil {
		slog.Default().Error("счётчик заданий на подпись не отвечает — задание всё равно выдаю",
			"source", "ratelimit",
			"ошибка", err,
		)
		return true
	}
	return allowed
}

func fold(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}
