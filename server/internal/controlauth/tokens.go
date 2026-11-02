package controlauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/atomicfile"
)

var ErrUnauthorized = errors.New("токен администратора недействителен или отозван")

type Token struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Scopes  []string  `json:"scopes"`
	Expires time.Time `json:"expires"`
	Hash    string    `json:"hash,omitempty"`
	Revoked bool      `json:"revoked,omitempty"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func NewStore(path string) *Store { return &Store{path: path} }

func (s *Store) load() ([]Token, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var tokens []Token
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, errors.New("хранилище административных токенов повреждено")
	}
	return tokens, nil
}

func (s *Store) save(tokens []Token) error {
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return atomicfile.Write(s.path, data, 0o600)
}

func (s *Store) Create(name string, scopes []string, ttl time.Duration) (Token, string, error) {
	token, raw, err := newToken(name, scopes, ttl)
	if err != nil {
		return Token{}, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return Token{}, "", err
	}
	if err := s.save(append(tokens, token)); err != nil {
		return Token{}, "", err
	}
	token.Hash = ""
	return token, raw, nil
}

func newToken(name string, scopes []string, ttl time.Duration) (Token, string, error) {
	if strings.TrimSpace(name) == "" || len(scopes) == 0 || ttl <= 0 {
		return Token{}, "", errors.New("нужны имя, права и положительный срок действия токена")
	}
	for _, scope := range scopes {
		if !validScope(scope) {
			return Token{}, "", fmt.Errorf("неизвестное право: %s", scope)
		}
	}
	id, secret := rand.Text(), rand.Text()
	raw := "laminara_" + id + "." + secret
	digest := sha256.Sum256([]byte(raw))
	token := Token{ID: id, Name: name, Scopes: slices.Clone(scopes), Expires: time.Now().Add(ttl), Hash: hex.EncodeToString(digest[:])}
	return token, raw, nil
}

func (s *Store) Bootstrap(name string, ttl time.Duration) (Token, string, error) {
	token, raw, err := newToken(name, []string{"admin"}, ttl)
	if err != nil {
		return Token{}, "", err
	}
	data, err := json.Marshal([]Token{token})
	if err != nil {
		return Token{}, "", err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return Token{}, "", err
	}
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return Token{}, "", fmt.Errorf("bootstrap требует нового хранилища токенов: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		os.Remove(s.path)
		return Token{}, "", err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		os.Remove(s.path)
		return Token{}, "", err
	}
	if err := file.Close(); err != nil {
		return Token{}, "", err
	}
	token.Hash = ""
	return token, raw, nil
}

func validScope(scope string) bool {
	if strings.HasPrefix(scope, "command:") {
		return len(scope) > len("command:") && !strings.ContainsAny(scope, " \t\r\n/*")
	}
	return slices.Contains([]string{"admin", "read", "logs", "settings", "builds", "accounts", "access", "signing", "modules", "lifecycle"}, scope)
}

func (s *Store) Authenticate(raw string) (Token, error) {
	id, _, ok := strings.Cut(strings.TrimPrefix(raw, "laminara_"), ".")
	if !ok || !strings.HasPrefix(raw, "laminara_") {
		return Token{}, ErrUnauthorized
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return Token{}, ErrUnauthorized
	}
	digest := sha256.Sum256([]byte(raw))
	for _, token := range tokens {
		if token.ID != id {
			continue
		}
		stored, err := hex.DecodeString(token.Hash)
		if err != nil || token.Revoked || !time.Now().Before(token.Expires) || subtle.ConstantTimeCompare(stored, digest[:]) != 1 {
			return Token{}, ErrUnauthorized
		}
		token.Hash = ""
		return token, nil
	}
	return Token{}, ErrUnauthorized
}

func (s *Store) List() ([]Token, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	for i := range tokens {
		tokens[i].Hash = ""
	}
	return tokens, err
}

func (s *Store) Active(actor Token) bool {
	if actor.ID == "local" {
		return true
	}
	tokens, err := s.List()
	if err != nil {
		return false
	}
	for _, token := range tokens {
		if token.ID == actor.ID {
			return !token.Revoked && time.Now().Before(token.Expires) && slices.Equal(token.Scopes, actor.Scopes)
		}
	}
	return false
}

func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	tokens, err := s.load()
	if err != nil {
		return err
	}
	for i := range tokens {
		if tokens[i].ID == id {
			tokens[i].Revoked = true
			return s.save(tokens)
		}
	}
	return errors.New("токен не найден")
}
