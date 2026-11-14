package yggdrasil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

func gameProfileKey(uuid string) string {
	return "laminara:ygg:profile:" + uuid
}

const profileTTL = 30 * 24 * time.Hour

func (r *redisSessions) putProfile(ctx context.Context, uuid string, identity auth.Identity) error {
	data, err := json.Marshal(identity)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, gameProfileKey(uuid), data, profileTTL).Err()
}

func (r *redisSessions) getProfile(ctx context.Context, uuid string) (auth.Identity, bool, error) {
	data, err := r.client.Get(ctx, gameProfileKey(uuid)).Bytes()
	if errors.Is(err, redis.Nil) {
		return auth.Identity{}, false, nil
	}
	if err != nil {
		return auth.Identity{}, false, err
	}
	var identity auth.Identity
	if err := json.Unmarshal(data, &identity); err != nil {
		return auth.Identity{}, false, err
	}
	return identity, true, nil
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
	left, err := r.client.PTTL(ctx, gameSessionKey(accessToken)).Result()
	if err != nil {
		return session{}, false, err
	}
	if left <= 0 {
		return session{}, false, nil
	}
	return session{
		clientToken: stored.ClientToken,
		identity:    stored.Identity,
		expiresAt:   time.Now().Add(left),
	}, true, nil
}

var rotateGameSession = redis.NewScript(`
 local data = redis.call('GET', KEYS[1])
 if not data then return 0 end
 local userKey = KEYS[3]
 redis.call('SET', KEYS[2], data, 'PX', ARGV[1])
 redis.call('DEL', KEYS[1])
 redis.call('SREM', userKey, ARGV[2])
 redis.call('SADD', userKey, ARGV[3])
 local remaining = redis.call('PTTL', userKey)
 if remaining < tonumber(ARGV[1]) then redis.call('PEXPIRE', userKey, ARGV[1]) end
 return data
`)

func (r *redisSessions) rotate(ctx context.Context, oldToken, newToken string, ttl time.Duration) (session, bool, error) {
	current, found, err := r.get(ctx, oldToken)
	if err != nil || !found {
		return session{}, false, err
	}
	result, err := rotateGameSession.Run(ctx, r.client, []string{gameSessionKey(oldToken), gameSessionKey(newToken), gameUserKey(current.identity.Username)}, ttl.Milliseconds(), oldToken, newToken).Result()
	if err != nil {
		return session{}, false, err
	}
	data, ok := result.(string)
	if !ok {
		return session{}, false, nil
	}
	var stored storedSession
	if err := json.Unmarshal([]byte(data), &stored); err != nil {
		return session{}, false, err
	}
	return session{clientToken: stored.ClientToken, identity: stored.Identity, expiresAt: time.Now().Add(ttl)}, true, nil
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

var removeGameUser = redis.NewScript(`
 local tokens = redis.call('SMEMBERS', KEYS[1])
 for _, token in ipairs(tokens) do redis.call('DEL', ARGV[1] .. token) end
 redis.call('DEL', KEYS[1])
 return 1
`)

func (r *redisSessions) removeUser(ctx context.Context, username string) error {
	return removeGameUser.Run(ctx, r.client, []string{gameUserKey(username)}, "laminara:ygg:session:").Err()
}

func gameJoinKey(key string) string {
	digest := sha256.Sum256([]byte(key))
	return "laminara:ygg:join:" + hex.EncodeToString(digest[:])
}

func (r *redisSessions) putJoin(ctx context.Context, key string, record joinRecord, ttl time.Duration) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, gameJoinKey(key), data, ttl).Err()
}

func (r *redisSessions) getJoin(ctx context.Context, key string) (joinRecord, bool, error) {
	data, err := r.client.Get(ctx, gameJoinKey(key)).Bytes()
	if errors.Is(err, redis.Nil) {
		return joinRecord{}, false, nil
	}
	if err != nil {
		return joinRecord{}, false, err
	}
	var record joinRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return joinRecord{}, false, err
	}
	return record, true, nil
}
