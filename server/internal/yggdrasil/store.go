package yggdrasil

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
)

type session struct {
	clientToken string
	identity    auth.Identity
	expiresAt   time.Time
}

type joinRecord struct {
	identity  auth.Identity
	expiresAt time.Time
}

type store struct {
	mu       sync.Mutex
	sessions map[string]session
	joins    map[string]joinRecord
	profiles map[string]auth.Identity
	now      func() time.Time
	remote   *redisSessions
}

func newStore(now func() time.Time, sessions *redis.Client) *store {
	built := &store{
		sessions: make(map[string]session),
		joins:    make(map[string]joinRecord),
		profiles: make(map[string]auth.Identity),
		now:      now,
	}
	if sessions != nil {
		built.remote = &redisSessions{client: sessions}
	}
	return built
}

const maxLiveRecords = 20_000

func (s *store) putSession(ctx context.Context, accessToken, clientToken string, identity auth.Identity, ttl time.Duration) {
	sess := session{clientToken: clientToken, identity: identity, expiresAt: s.now().Add(ttl)}
	if s.remote != nil {
		if err := s.remote.put(ctx, accessToken, sess, ttl); err != nil {
			slog.Error("игровая сессия не записалась в redis", "source", "yggdrasil", "ошибка", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= maxLiveRecords {
		s.sweepLocked()
	}
	s.sessions[accessToken] = sess
}

func (s *store) sweepLocked() {
	now := s.now()
	for token, sess := range s.sessions {
		if now.After(sess.expiresAt) {
			delete(s.sessions, token)
		}
	}
	for id, record := range s.joins {
		if now.After(record.expiresAt) {
			delete(s.joins, id)
		}
	}
}

func (s *store) session(ctx context.Context, accessToken string) (session, bool) {
	if s.remote != nil {
		sess, found, err := s.remote.get(ctx, accessToken)
		if err != nil {
			slog.Error("игровая сессия не читается из redis", "source", "yggdrasil", "ошибка", err)
		}
		return sess, found
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[accessToken]
	if !ok {
		return session{}, false
	}
	if s.now().After(sess.expiresAt) {
		delete(s.sessions, accessToken)
		return session{}, false
	}
	return sess, true
}

func (s *store) rotateSession(ctx context.Context, oldToken, newToken string, ttl time.Duration) (session, bool) {
	if s.remote != nil {
		sess, rotated, err := s.remote.rotate(ctx, oldToken, newToken, ttl)
		if err != nil {
			slog.Error("игровая сессия не продлилась в redis", "source", "yggdrasil", "ошибка", err)
		}
		return sess, rotated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[oldToken]
	if !ok {
		return session{}, false
	}
	delete(s.sessions, oldToken)
	sess.expiresAt = s.now().Add(ttl)
	s.sessions[newToken] = sess
	return sess, true
}

func (s *store) deleteSession(ctx context.Context, accessToken string) {
	if s.remote != nil {
		if err := s.remote.remove(ctx, accessToken); err != nil {
			slog.Error("игровая сессия не удалилась из redis", "source", "yggdrasil", "ошибка", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, accessToken)
}

func (s *store) deleteUser(ctx context.Context, username string) {
	if s.remote != nil {
		if err := s.remote.removeUser(ctx, username); err != nil {
			slog.Error("сессии игрока не удалились из redis", "source", "yggdrasil", "ошибка", err)
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if strings.EqualFold(sess.identity.Username, username) {
			delete(s.sessions, token)
		}
	}
}

func (s *store) putJoin(serverID string, identity auth.Identity, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.joins) >= maxLiveRecords {
		s.sweepLocked()
	}
	s.joins[serverID] = joinRecord{identity: identity, expiresAt: s.now().Add(ttl)}
}

func (s *store) join(serverID string) (auth.Identity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.joins[serverID]
	if !ok {
		return auth.Identity{}, false
	}
	if s.now().After(record.expiresAt) {
		delete(s.joins, serverID)
		return auth.Identity{}, false
	}
	return record.identity, true
}

func (s *store) rememberProfile(identity auth.Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.profiles[dashless(identity.UUID)] = identity
}

func (s *store) profile(uuid string) (auth.Identity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	identity, ok := s.profiles[uuid]
	return identity, ok
}
