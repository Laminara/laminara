package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
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
	Disabled  bool   `json:"disabled"`
	Backend   string `json:"backend"`
	RedisAddr string `json:"redisAddr"`

	Login     Bucket `json:"login"`
	Account   Bucket `json:"account"`
	Challenge Bucket `json:"challenge"`
}

type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

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
		if resolved.RedisAddr == "" {
			return nil, fmt.Errorf("rateLimit.backend is redis but rateLimit.redisAddr is empty")
		}
		client := redis.NewClient(&redis.Options{Addr: resolved.RedisAddr})
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
