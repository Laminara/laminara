package yggdrasil

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
)

func redisStore(t *testing.T) (*store, *miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	return newStore(time.Now, client), server, client
}

func TestAGameSessionOutlivesARestartOfTheServer(t *testing.T) {
	ctx := context.Background()
	first, _, client := redisStore(t)
	identity := auth.Identity{Username: "Ivan"}
	first.putSession(ctx, "access-1", "client-1", identity, time.Hour)

	restarted := newStore(time.Now, client)
	sess, ok := restarted.session(ctx, "access-1")
	if !ok {
		t.Fatal("после перезапуска сервера игрока выкинуло из аккаунта — сессия не пережила рестарт")
	}
	if sess.identity.Username != "Ivan" || sess.clientToken != "client-1" {
		t.Fatalf("сессия вернулась испорченной: %+v", sess)
	}
}

func TestAGameSessionInMemoryDoesNotOutliveARestart(t *testing.T) {
	ctx := context.Background()
	first := newStore(time.Now, nil)
	first.putSession(ctx, "access-1", "client-1", auth.Identity{Username: "Ivan"}, time.Hour)

	restarted := newStore(time.Now, nil)
	if _, ok := restarted.session(ctx, "access-1"); ok {
		t.Fatal("сессия в памяти не может пережить рестарт — иначе тест ничего не проверяет")
	}
}

func TestRotationKeepsThePlayerAndForgetsTheOldToken(t *testing.T) {
	ctx := context.Background()
	keeper, _, _ := redisStore(t)
	keeper.putSession(ctx, "access-1", "client-1", auth.Identity{Username: "Ivan"}, time.Hour)

	sess, rotated := keeper.rotateSession(ctx, "access-1", "access-2", time.Hour)
	if !rotated || sess.identity.Username != "Ivan" {
		t.Fatalf("продление сессии не прошло: %+v %v", sess, rotated)
	}
	if _, ok := keeper.session(ctx, "access-1"); ok {
		t.Fatal("старый токен обязан перестать работать после продления")
	}
	if _, ok := keeper.session(ctx, "access-2"); !ok {
		t.Fatal("новый токен не работает")
	}
}

func TestSignOutDropsEverySessionOfThatPlayer(t *testing.T) {
	ctx := context.Background()
	keeper, _, _ := redisStore(t)
	keeper.putSession(ctx, "access-1", "client-1", auth.Identity{Username: "Ivan"}, time.Hour)
	keeper.putSession(ctx, "access-2", "client-2", auth.Identity{Username: "ivan"}, time.Hour)
	keeper.putSession(ctx, "access-3", "client-3", auth.Identity{Username: "Пётр"}, time.Hour)

	keeper.deleteUser(ctx, "IVAN")
	for _, token := range []string{"access-1", "access-2"} {
		if _, ok := keeper.session(ctx, token); ok {
			t.Fatalf("после выхода токен %s всё ещё пускает в игру", token)
		}
	}
	if _, ok := keeper.session(ctx, "access-3"); !ok {
		t.Fatal("выход одного игрока не должен ронять сессии другого")
	}
}

func TestAnExpiredSessionIsGone(t *testing.T) {
	ctx := context.Background()
	keeper, server, _ := redisStore(t)
	keeper.putSession(ctx, "access-1", "client-1", auth.Identity{Username: "Ivan"}, time.Minute)

	server.FastForward(2 * time.Minute)
	if _, ok := keeper.session(ctx, "access-1"); ok {
		t.Fatal("протухшая сессия обязана перестать работать")
	}
}
