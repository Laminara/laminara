package yggdrasil

import (
	"strings"
	"sync"
	"time"

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
}

func newStore(now func() time.Time) *store {
	return &store{
		sessions: make(map[string]session),
		joins:    make(map[string]joinRecord),
		profiles: make(map[string]auth.Identity),
		now:      now,
	}
}

func (s *store) putSession(accessToken, clientToken string, identity auth.Identity, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[accessToken] = session{clientToken: clientToken, identity: identity, expiresAt: s.now().Add(ttl)}
}

func (s *store) session(accessToken string) (session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[accessToken]
	if !ok || s.now().After(sess.expiresAt) {
		return session{}, false
	}
	return sess, true
}

func (s *store) rotateSession(oldToken, newToken string, ttl time.Duration) (session, bool) {
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

func (s *store) deleteSession(accessToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, accessToken)
}

func (s *store) deleteUser(username string) {
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
	s.joins[serverID] = joinRecord{identity: identity, expiresAt: s.now().Add(ttl)}
}

func (s *store) join(serverID string) (auth.Identity, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.joins[serverID]
	if !ok || s.now().After(record.expiresAt) {
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
