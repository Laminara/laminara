package ratelimit

import "context"

func (l *RedisLimiter) Readiness(ctx context.Context) error { return l.client.Ping(ctx).Err() }

func (g *Guard) Readiness(ctx context.Context) error {
	if g == nil {
		return nil
	}
	for _, limiter := range []Limiter{g.login, g.account, g.challenge} {
		if ready, ok := limiter.(interface{ Readiness(context.Context) error }); ok {
			if err := ready.Readiness(ctx); err != nil {
				return err
			}
		}
	}
	return nil
}
