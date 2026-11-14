package yggdrasil

import (
	"context"
	"fmt"
	"os"
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
	sess, ok, _ := restarted.session(ctx, "access-1")
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
	if _, ok, _ := restarted.session(ctx, "access-1"); ok {
		t.Fatal("сессия в памяти не может пережить рестарт — иначе тест ничего не проверяет")
	}
}

func TestRotationKeepsThePlayerAndForgetsTheOldToken(t *testing.T) {
	ctx := context.Background()
	keeper, _, _ := redisStore(t)
	keeper.putSession(ctx, "access-1", "client-1", auth.Identity{Username: "Ivan"}, time.Hour)

	sess, rotated, _ := keeper.rotateSession(ctx, "access-1", "access-2", time.Hour)
	if !rotated || sess.identity.Username != "Ivan" {
		t.Fatalf("продление сессии не прошло: %+v %v", sess, rotated)
	}
	if _, ok, _ := keeper.session(ctx, "access-1"); ok {
		t.Fatal("старый токен обязан перестать работать после продления")
	}
	if _, ok, _ := keeper.session(ctx, "access-2"); !ok {
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
		if _, ok, _ := keeper.session(ctx, token); ok {
			t.Fatalf("после выхода токен %s всё ещё пускает в игру", token)
		}
	}
	if _, ok, _ := keeper.session(ctx, "access-3"); !ok {
		t.Fatal("выход одного игрока не должен ронять сессии другого")
	}
}

func TestAnExpiredSessionIsGone(t *testing.T) {
	ctx := context.Background()
	keeper, server, _ := redisStore(t)
	keeper.putSession(ctx, "access-1", "client-1", auth.Identity{Username: "Ivan"}, time.Minute)

	server.FastForward(2 * time.Minute)
	if _, ok, _ := keeper.session(ctx, "access-1"); ok {
		t.Fatal("протухшая сессия обязана перестать работать")
	}
}

func TestConcurrentRefreshOnlyRotatesOnce(t *testing.T) {
	keeper, _, _ := redisStore(t)
	ctx := context.Background()
	keeper.putSession(ctx, "original", "client", auth.Identity{Username: "Alice"}, time.Hour)
	results := make(chan bool, 2)
	for _, token := range []string{"first", "second"} {
		go func() { _, ok, _ := keeper.rotateSession(ctx, "original", token, time.Hour); results <- ok }()
	}
	successes := 0
	for range 2 {
		if <-results {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent refresh issued %d tokens", successes)
	}
}

func TestJoinsAreSharedAndRevocationPreventsHandshake(t *testing.T) {
	ctx := context.Background()
	first, clock, client := redisStore(t)
	second := newStore(time.Now, client)
	identity := auth.Identity{Username: "Alice", UUID: auth.OfflineUUID("Alice")}
	if err := first.putSession(ctx, "token", "client", identity, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := first.putJoin(ctx, "challenge", "token", "127.0.0.1", identity, time.Second); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := second.join(ctx, "challenge", "alice", "::ffff:127.0.0.1"); err != nil || !ok {
		t.Fatalf("shared join: %v %v", ok, err)
	}
	if _, ok, err := second.join(ctx, "challenge", "alice", "192.0.2.1"); err != nil || ok {
		t.Fatalf("wrong IP: %v %v", ok, err)
	}
	if err := first.deleteSession(ctx, "token"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := second.join(ctx, "challenge", "alice", ""); err != nil || ok {
		t.Fatalf("revoked join: %v %v", ok, err)
	}
	if err := first.putSession(ctx, "token", "client", identity, time.Hour); err != nil {
		t.Fatal(err)
	}
	clock.FastForward(2 * time.Second)
	if _, ok, err := second.join(ctx, "challenge", "alice", ""); err != nil || ok {
		t.Fatalf("expired join: %v %v", ok, err)
	}
}

func TestConcurrentSignoutAndRefreshLeaveNoSessions(t *testing.T) {
	ctx := context.Background()
	keeper, _, _ := redisStore(t)
	for range 20 {
		if err := keeper.putSession(ctx, "old", "client", auth.Identity{Username: "Игрок"}, time.Hour); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 2)
		go func() { _, _, err := keeper.rotateSession(ctx, "old", "new", time.Hour); done <- err }()
		go func() { done <- keeper.deleteUser(ctx, "Игрок") }()
		for range 2 {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		for _, token := range []string{"old", "new"} {
			if _, ok, err := keeper.session(ctx, token); err != nil || ok {
				t.Fatalf("session survived signout: %s %v %v", token, ok, err)
			}
		}
	}
}

func TestLiveRedisRefreshAndSignout(t *testing.T) {
	address := os.Getenv("LAMINARA_TEST_REDIS_ADDR")
	if address == "" {
		t.Skip("set LAMINARA_TEST_REDIS_ADDR to a disposable Redis")
	}
	client := redis.NewClient(&redis.Options{Addr: address})
	defer client.Close()
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	keeper := newStore(time.Now, client)
	user := fmt.Sprintf("ygg-review-%d", time.Now().UnixNano())
	identity := auth.Identity{Username: user, UUID: auth.OfflineUUID(user)}
	defer keeper.deleteUser(ctx, user)
	for i := range 20 {
		old, new := fmt.Sprintf("%s-old-%d", user, i), fmt.Sprintf("%s-new-%d", user, i)
		if err := keeper.putSession(ctx, old, "client", identity, time.Minute); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 2)
		go func() { _, _, err := keeper.rotateSession(ctx, old, new, time.Minute); done <- err }()
		go func() { done <- keeper.deleteUser(ctx, user) }()
		for range 2 {
			if err := <-done; err != nil {
				t.Fatal(err)
			}
		}
		for _, token := range []string{old, new} {
			if _, ok, err := keeper.session(ctx, token); err != nil || ok {
				t.Fatalf("live Redis signout lost race: %v %v", ok, err)
			}
		}
	}
}
