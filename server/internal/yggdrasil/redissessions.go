package yggdrasil

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
)

type redisSessions struct {
	client *redis.Client
}

type storedSession struct {
	ClientToken string        `json:"clientToken"`
	Identity    auth.Identity `json:"identity"`
	Username    string        `json:"username"`
}

func gameSessionKey(accessToken string) string {
	return "laminara:ygg:session:" + accessToken
}

func gameUserKey(username string) string {
	return "laminara:ygg:user:" + strings.ToLower(username)
}

func (r *redisSessions) put(ctx context.Context, accessToken string, sess session, ttl time.Duration) error {
	data, err := json.Marshal(storedSession{
		ClientToken: sess.clientToken,
		Identity:    sess.identity,
		Username:    sess.identity.Username,
	})
	if err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Set(ctx, gameSessionKey(accessToken), data, ttl)
	userKey := gameUserKey(sess.identity.Username)
	pipe.SAdd(ctx, userKey, accessToken)
	pipe.Expire(ctx, userKey, ttl)
	_, err = pipe.Exec(ctx)
	return err
}

func (r *redisSessions) get(ctx context.Context, accessToken string) (session, bool, error) {
	data, err := r.client.Get(ctx, gameSessionKey(accessToken)).Bytes()
	if errors.Is(err, redis.Nil) {
		return session{}, false, nil
	}
	if err != nil {
		return session{}, false, err
	}
	var stored storedSession
	if err := json.Unmarshal(data, &stored); err != nil {
		return session{}, false, err
	}
	left, err := r.client.TTL(ctx, gameSessionKey(accessToken)).Result()
	if err != nil {
		return session{}, false, err
	}
	return session{
		clientToken: stored.ClientToken,
		identity:    stored.Identity,
		expiresAt:   time.Now().Add(left),
	}, true, nil
}

func (r *redisSessions) rotate(ctx context.Context, oldToken, newToken string, ttl time.Duration) (session, bool, error) {
	sess, found, err := r.get(ctx, oldToken)
	if err != nil || !found {
		return session{}, false, err
	}
	if err := r.put(ctx, newToken, sess, ttl); err != nil {
		return session{}, false, err
	}
	if err := r.remove(ctx, oldToken); err != nil {
		return session{}, false, err
	}
	sess.expiresAt = time.Now().Add(ttl)
	return sess, true, nil
}

func (r *redisSessions) remove(ctx context.Context, accessToken string) error {
	sess, found, err := r.get(ctx, accessToken)
	if err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	pipe.Del(ctx, gameSessionKey(accessToken))
	if found {
		pipe.SRem(ctx, gameUserKey(sess.identity.Username), accessToken)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (r *redisSessions) removeUser(ctx context.Context, username string) error {
	userKey := gameUserKey(username)
	tokens, err := r.client.SMembers(ctx, userKey).Result()
	if err != nil {
		return err
	}
	pipe := r.client.TxPipeline()
	for _, token := range tokens {
		pipe.Del(ctx, gameSessionKey(token))
	}
	pipe.Del(ctx, userKey)
	_, err = pipe.Exec(ctx)
	return err
}
