package ratelimit

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisLimiter struct {
	client *redis.Client
	limit  int
	window time.Duration
}

func NewRedisLimiter(client *redis.Client, limit int, per time.Duration) *RedisLimiter {
	return &RedisLimiter{client: client, limit: limit, window: per}
}

func (l *RedisLimiter) Blocked(ctx context.Context, key string) (bool, error) {
	count, err := l.client.Get(ctx, "laminara:ratelimit:"+key).Int64()
	if err == redis.Nil {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return count >= int64(l.limit), nil
}

func (l *RedisLimiter) Allow(ctx context.Context, key string) (bool, error) {
	full := "laminara:ratelimit:" + key
	fresh, err := l.client.SetArgs(ctx, full, 1, redis.SetArgs{Mode: "NX", TTL: l.window}).Result()
	if err != nil && err != redis.Nil {
		return false, err
	}
	if fresh == "OK" {
		return l.limit >= 1, nil
	}
	count, err := l.client.Incr(ctx, full).Result()
	if err != nil {
		return false, err
	}
	if count == 1 {
		if err := l.client.Expire(ctx, full, l.window).Err(); err != nil {
			return false, err
		}
	}
	return count <= int64(l.limit), nil
}
