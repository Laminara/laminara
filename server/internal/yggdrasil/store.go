package yggdrasil

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
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
	Identity    auth.Identity
	AccessToken string
	IP          string
	expiresAt   time.Time
}

type profileRecord struct {
	identity  auth.Identity
	expiresAt time.Time
}

type store struct {
	mu       sync.Mutex
	sessions map[string]session
	joins    map[string]joinRecord
	profiles map[string]profileRecord
	now      func() time.Time
	remote   *redisSessions
}

func newStore(now func() time.Time, sessions *redis.Client) *store {
	built := &store{
		sessions: make(map[string]session),
		joins:    make(map[string]joinRecord),
		profiles: make(map[string]profileRecord),
		now:      now,
	}
	if sessions != nil {
		built.remote = &redisSessions{client: sessions}
	}
	return built
}

const maxLiveRecords = 20_000

func (s *store) putSession(ctx context.Context, accessToken, clientToken string, identity auth.Identity, ttl time.Duration) error {
	sess := session{clientToken: clientToken, identity: identity, expiresAt: s.now().Add(ttl)}
	if s.remote != nil {
		return s.remote.put(ctx, accessToken, sess, ttl)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.sessions) >= maxLiveRecords {
		s.sweepLocked()
	}
	if len(s.sessions) >= maxLiveRecords {
		return fmt.Errorf("достигнут предел игровых сессий")
	}
	s.sessions[accessToken] = sess
	return nil
}

func (s *store) sweepLocked() {
	now := s.now()
	for token, sess := range s.sessions {
		if !now.Before(sess.expiresAt) {
			delete(s.sessions, token)
		}
	}
	for id, record := range s.joins {
		if !now.Before(record.expiresAt) {
			delete(s.joins, id)
		}
	}
}

func (s *store) session(ctx context.Context, accessToken string) (session, bool, error) {
	if s.remote != nil {
		return s.remote.get(ctx, accessToken)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[accessToken]
	if !ok {
		return session{}, false, nil
	}
	if !s.now().Before(sess.expiresAt) {
		delete(s.sessions, accessToken)
		return session{}, false, nil
	}
	return sess, true, nil
}

func (s *store) rotateSession(ctx context.Context, oldToken, newToken string, ttl time.Duration) (session, bool, error) {
	if s.remote != nil {
		return s.remote.rotate(ctx, oldToken, newToken, ttl)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[oldToken]
	if !ok || !s.now().Before(sess.expiresAt) {
		return session{}, false, nil
	}
	delete(s.sessions, oldToken)
	sess.expiresAt = s.now().Add(ttl)
	s.sessions[newToken] = sess
	return sess, true, nil
}

func (s *store) deleteSession(ctx context.Context, accessToken string) error {
	if s.remote != nil {
		return s.remote.remove(ctx, accessToken)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, accessToken)
	return nil
}

func (s *store) deleteUser(ctx context.Context, username string) error {
	if s.remote != nil {
		return s.remote.removeUser(ctx, username)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if strings.EqualFold(sess.identity.Username, username) {
			delete(s.sessions, token)
		}
	}
	return nil
}

func joinKey(serverID, username string) string {
	return fmt.Sprintf("%d:%s%s", len(serverID), serverID, strings.ToLower(username))
}

func (s *store) putJoin(ctx context.Context, serverID, token, ip string, identity auth.Identity, ttl time.Duration) error {
	record := joinRecord{Identity: identity, AccessToken: token, IP: ip, expiresAt: s.now().Add(ttl)}
	key := joinKey(serverID, identity.Username)
	if s.remote != nil {
		return s.remote.putJoin(ctx, key, record, ttl)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.joins) >= maxLiveRecords {
		s.sweepLocked()
	}
	if len(s.joins) >= maxLiveRecords {
		return fmt.Errorf("достигнут предел ожидающих подключений")
	}
	s.joins[key] = record
	return nil
}

func (s *store) join(ctx context.Context, serverID, username, ip string) (auth.Identity, bool, error) {
	key := joinKey(serverID, username)
	var record joinRecord
	var ok bool
	if s.remote != nil {
		var err error
		record, ok, err = s.remote.getJoin(ctx, key)
		if err != nil {
			return auth.Identity{}, false, err
		}
	} else {
		s.mu.Lock()
		record, ok = s.joins[key]
		if ok && !s.now().Before(record.expiresAt) {
			delete(s.joins, key)
			ok = false
		}
		s.mu.Unlock()
	}
	if ip != "" {
		expected, expectedErr := netip.ParseAddr(ip)
		actual, actualErr := netip.ParseAddr(record.IP)
		if expectedErr != nil || actualErr != nil || expected.Unmap() != actual.Unmap() {
			return auth.Identity{}, false, nil
		}
	}
	if !ok {
		return auth.Identity{}, false, nil
	}
	sess, valid, err := s.session(ctx, record.AccessToken)
	if err != nil || !valid {
		return auth.Identity{}, false, err
	}
	if sess.identity.UUID != record.Identity.UUID || !strings.EqualFold(sess.identity.Username, username) {
		return auth.Identity{}, false, nil
	}
	return record.Identity, true, nil
}

func (s *store) rememberProfile(ctx context.Context, identity auth.Identity) {
	key := dashless(identity.UUID)
	if s.remote == nil {
		s.mu.Lock()
		if len(s.profiles) >= maxLiveRecords {
			for k := range s.profiles {
				delete(s.profiles, k)
				break
			}
		}
		s.profiles[key] = profileRecord{identity: identity, expiresAt: s.now().Add(profileTTL)}
		s.mu.Unlock()
		return
	}
	if err := s.remote.putProfile(ctx, key, identity); err != nil {
		slog.Error("профиль игрока не записался в redis", "source", "yggdrasil", "ошибка", err)
	}
}

func (s *store) profile(ctx context.Context, uuid string) (auth.Identity, bool, error) {
	if s.remote != nil {
		return s.remote.getProfile(ctx, uuid)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.profiles[uuid]
	if ok && !s.now().Before(record.expiresAt) {
		delete(s.profiles, uuid)
		ok = false
	}
	return record.identity, ok, nil
}
