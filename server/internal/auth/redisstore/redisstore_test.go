package redisstore_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/redisstore"
)

func newStore(t *testing.T) *redisstore.SessionStore {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	return redisstore.NewSessionStore(client, time.Hour)
}

func TestCreateAndGet(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	session := &auth.Session{
		ID:               uuid.New(),
		Identity:         auth.Identity{Username: "neo"},
		AccessTokenHash:  "access",
		RefreshTokenHash: "refresh",
	}
	if err := store.Create(ctx, session); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity.Username != "neo" || got.AccessTokenHash != "access" {
		t.Fatalf("got %+v", got)
	}
}

func TestGetMissing(t *testing.T) {
	if _, err := newStore(t).Get(context.Background(), uuid.New()); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestUpdateMissing(t *testing.T) {
	if err := newStore(t).Update(context.Background(), &auth.Session{ID: uuid.New()}); !errors.Is(err, auth.ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestRevoke(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	session := &auth.Session{ID: uuid.New()}
	if err := store.Create(ctx, session); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(ctx, session.ID); err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(ctx, session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Revoked {
		t.Fatal("session not revoked")
	}
}
