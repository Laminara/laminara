package auth

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrSessionNotFound = errors.New("session not found")

type Session struct {
	ID               uuid.UUID
	Identity         Identity
	AccessTokenHash  string
	AccessExpiresAt  time.Time
	RefreshTokenHash string
	ExpiresAt        time.Time
	Revoked          bool
}

type SessionStore interface {
	Create(ctx context.Context, session *Session) error
	Get(ctx context.Context, id uuid.UUID) (*Session, error)
	Update(ctx context.Context, session *Session) error
	Revoke(ctx context.Context, id uuid.UUID) error
}

var _ SessionStore = (*MemorySessionStore)(nil)

type MemorySessionStore struct {
	mu       sync.Mutex
	sessions map[uuid.UUID]Session
}

func NewMemorySessionStore() *MemorySessionStore {
	return &MemorySessionStore{sessions: make(map[uuid.UUID]Session)}
}

func (m *MemorySessionStore) Create(_ context.Context, session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = *session
	return nil
}

func (m *MemorySessionStore) Get(_ context.Context, id uuid.UUID) (*Session, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return nil, ErrSessionNotFound
	}
	return &session, nil
}

func (m *MemorySessionStore) Update(_ context.Context, session *Session) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.sessions[session.ID]; !ok {
		return ErrSessionNotFound
	}
	m.sessions[session.ID] = *session
	return nil
}

func (m *MemorySessionStore) Revoke(_ context.Context, id uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	session, ok := m.sessions[id]
	if !ok {
		return ErrSessionNotFound
	}
	session.Revoked = true
	m.sessions[id] = session
	return nil
}
