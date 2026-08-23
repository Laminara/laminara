package ratelimit

import (
	"context"
	"sync"
	"time"
)

type Limiter interface {
	Allow(ctx context.Context, key string) (bool, error)
	Blocked(ctx context.Context, key string) (bool, error)
}

var (
	_ Limiter = (*MemoryLimiter)(nil)
	_ Limiter = (*RedisLimiter)(nil)
)

type window struct {
	count   int
	resetAt time.Time
}

type MemoryLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	now     func() time.Time
	entries map[string]window
}

func NewMemoryLimiter(limit int, per time.Duration) *MemoryLimiter {
	return &MemoryLimiter{
		limit:   limit,
		window:  per,
		now:     time.Now,
		entries: make(map[string]window),
	}
}

func (l *MemoryLimiter) Blocked(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry, ok := l.entries[key]
	if !ok || l.now().After(entry.resetAt) {
		return false, nil
	}
	return entry.count >= l.limit, nil
}

func (l *MemoryLimiter) Allow(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	entry, ok := l.entries[key]
	if !ok || now.After(entry.resetAt) {
		l.entries[key] = window{count: 1, resetAt: now.Add(l.window)}
		return l.limit >= 1, nil
	}
	entry.count++
	l.entries[key] = entry
	return entry.count <= l.limit, nil
}
