package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestMemoryLimiterAllowsThenDenies(t *testing.T) {
	current := time.Unix(0, 0)
	limiter := NewMemoryLimiter(2, time.Minute)
	limiter.now = func() time.Time { return current }
	ctx := context.Background()

	for i, want := range []bool{true, true, false} {
		got, err := limiter.Allow(ctx, "user")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("attempt %d: got %v want %v", i, got, want)
		}
	}

	current = current.Add(2 * time.Minute)
	if got, _ := limiter.Allow(ctx, "user"); !got {
		t.Fatal("window should have reset")
	}
}

func TestRedisLimiter(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	limiter := NewRedisLimiter(client, 2, time.Minute)
	ctx := context.Background()

	for i, want := range []bool{true, true, false} {
		got, err := limiter.Allow(ctx, "user")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("attempt %d: got %v want %v", i, got, want)
		}
	}

	server.FastForward(2 * time.Minute)
	if got, _ := limiter.Allow(ctx, "user"); !got {
		t.Fatal("window should have expired")
	}
}
