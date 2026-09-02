package webconsole

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const secretBytes = 32

var errUnknownTicket = errors.New("ссылка уже использована или устарела")

type record struct {
	Hash      string    `json:"hash"`
	ExpiresAt time.Time `json:"expiresAt,omitzero"`
	IssuedAt  time.Time `json:"issuedAt"`
	Address   string    `json:"address,omitempty"`
}

type store struct {
	path string

	mu       sync.Mutex
	tickets  map[string]record
	sessions map[string]record
}

func newStore(path string) *store {
	s := &store{path: path, tickets: map[string]record{}, sessions: map[string]record{}}
	s.load()
	return s
}

func (s *store) ticket(ttl time.Duration) string {
	secret := mint()
	s.mu.Lock()
	s.tickets[digest(secret)] = record{Hash: digest(secret), IssuedAt: time.Now(), ExpiresAt: time.Now().Add(ttl)}
	s.mu.Unlock()
	return secret
}

func (s *store) redeem(secret, address string, ttl time.Duration) (string, error) {
	key := digest(secret)
	s.mu.Lock()
	defer s.mu.Unlock()

	entry, ok := s.tickets[key]
	delete(s.tickets, key)
	if !ok || expired(entry) {
		return "", errUnknownTicket
	}

	session := mint()
	created := record{Hash: digest(session), IssuedAt: time.Now(), Address: address}
	if ttl > 0 {
		created.ExpiresAt = time.Now().Add(ttl)
	}
	s.sessions[created.Hash] = created
	s.saveLocked()
	return session, nil
}

func (s *store) open(address string, ttl time.Duration) string {
	session := mint()
	created := record{Hash: digest(session), IssuedAt: time.Now(), Address: address}
	if ttl > 0 {
		created.ExpiresAt = time.Now().Add(ttl)
	}
	s.mu.Lock()
	s.sessions[created.Hash] = created
	s.saveLocked()
	s.mu.Unlock()
	return session
}

func (s *store) valid(secret string) bool {
	if secret == "" {
		return false
	}
	key := digest(secret)
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.sessions[key]
	if !ok {
		return false
	}
	if expired(entry) {
		delete(s.sessions, key)
		s.saveLocked()
		return false
	}
	return subtle.ConstantTimeCompare([]byte(entry.Hash), []byte(key)) == 1
}

func (s *store) close(secret string) {
	if secret == "" {
		return
	}
	s.mu.Lock()
	delete(s.sessions, digest(secret))
	s.saveLocked()
	s.mu.Unlock()
}

func (s *store) forgetAll() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := len(s.sessions)
	s.sessions = map[string]record{}
	s.tickets = map[string]record{}
	s.saveLocked()
	return count
}

func (s *store) live() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	live := 0
	for key, entry := range s.sessions {
		if expired(entry) {
			delete(s.sessions, key)
			continue
		}
		live++
	}
	return live
}

func (s *store) load() {
	if s.path == "" {
		return
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var saved []record
	if err := json.Unmarshal(data, &saved); err != nil {
		return
	}
	for _, entry := range saved {
		if !expired(entry) {
			s.sessions[entry.Hash] = entry
		}
	}
}

func (s *store) saveLocked() {
	if s.path == "" {
		return
	}
	saved := make([]record, 0, len(s.sessions))
	for _, entry := range s.sessions {
		saved = append(saved, entry)
	}
	data, err := json.Marshal(saved)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	os.Rename(tmp, s.path)
}

func expired(entry record) bool {
	return !entry.ExpiresAt.IsZero() && time.Now().After(entry.ExpiresAt)
}

func mint() string {
	secret := make([]byte, secretBytes)
	if _, err := rand.Read(secret); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(secret)
}

func digest(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}
