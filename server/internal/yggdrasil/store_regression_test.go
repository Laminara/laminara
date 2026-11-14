package yggdrasil

import (
	"context"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/auth"
)

func TestJoinRecordsDoNotOverwriteOtherPlayers(t *testing.T) {
	now := time.Now()
	keeper := newStore(func() time.Time { return now }, nil)
	for _, name := range []string{"Alice", "Bob"} {
		identity := auth.Identity{Username: name, UUID: auth.OfflineUUID(name)}
		keeper.putSession(context.Background(), name, "client", identity, time.Hour)
		keeper.putJoin(context.Background(), "same-challenge", name, "127.0.0.1", identity, time.Second)
	}
	for _, name := range []string{"Alice", "Bob"} {
		id, ok, _ := keeper.join(context.Background(), "same-challenge", name, "")
		if !ok || id.Username != name {
			t.Fatalf("join lost: %s %+v", name, id)
		}
	}
	if _, ok, _ := keeper.join(context.Background(), "same-challenge", "Mallory", ""); ok {
		t.Fatal("unknown player accepted")
	}
	now = now.Add(2 * time.Second)
	if _, ok, _ := keeper.join(context.Background(), "same-challenge", "Alice", ""); ok {
		t.Fatal("expired join accepted")
	}
}

func TestExpiredMemorySessionCannotBeRotated(t *testing.T) {
	now := time.Now()
	keeper := newStore(func() time.Time { return now }, nil)
	keeper.putSession(context.Background(), "old", "client", auth.Identity{Username: "Alice"}, time.Second)
	now = now.Add(2 * time.Second)
	if _, ok, _ := keeper.rotateSession(context.Background(), "old", "new", time.Hour); ok {
		t.Fatal("expired token resurrected")
	}
}
