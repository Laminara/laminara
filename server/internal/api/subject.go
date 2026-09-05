package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/access"
)

const (
	subjectTTL      = 15 * time.Second
	maxCachedTokens = 10_000
)

type cachedSubject struct {
	subject access.Subject
	expires time.Time
}

type tokenCache struct {
	mu      sync.Mutex
	entries map[string]cachedSubject
}

func newTokenCache() *tokenCache {
	return &tokenCache{entries: map[string]cachedSubject{}}
}

func tokenFingerprint(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (c *tokenCache) get(token string) (access.Subject, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[tokenFingerprint(token)]
	if !ok || time.Now().After(entry.expires) {
		return access.Subject{}, false
	}
	return entry.subject, true
}

func (c *tokenCache) put(token string, subject access.Subject) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= maxCachedTokens {
		now := time.Now()
		for key, entry := range c.entries {
			if now.After(entry.expires) {
				delete(c.entries, key)
			}
		}
		target := maxCachedTokens * 7 / 8
		for key := range c.entries {
			if len(c.entries) <= target {
				break
			}
			delete(c.entries, key)
		}
	}
	c.entries[tokenFingerprint(token)] = cachedSubject{subject: subject, expires: time.Now().Add(subjectTTL)}
}

func bearerToken(header http.Header) string {
	value := header.Get("Authorization")
	if value == "" {
		return ""
	}
	if token, ok := strings.CutPrefix(value, "Bearer "); ok {
		return strings.TrimSpace(token)
	}
	return ""
}

type tokenState int

const (
	tokenAbsent tokenState = iota
	tokenValid
	tokenStale
)

var errStaleSession = errors.New("session expired, sign in again")

func (s *Service) subjectOf(ctx context.Context, header http.Header) (access.Subject, tokenState) {
	token := bearerToken(header)
	if token == "" || s.auth == nil {
		return access.Subject{}, tokenAbsent
	}
	if cached, ok := s.tokens.get(token); ok {
		return cached, tokenValid
	}
	identity, err := s.auth.ValidateAccess(ctx, token)
	if err != nil {
		return access.Subject{}, tokenStale
	}
	subject := access.Subject{Subject: identity.Subject, Username: identity.Username, UUID: identity.UUID.String()}
	s.tokens.put(token, subject)
	return subject, tokenValid
}
