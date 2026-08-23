package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
)

var _ auth.SessionStore = (*SessionStore)(nil)

type SessionStore struct {
	client *redis.Client
	ttl    time.Duration
}

func NewSessionStore(client *redis.Client, ttl time.Duration) *SessionStore {
	return &SessionStore{client: client, ttl: ttl}
}

func sessionKey(id uuid.UUID) string {
	return "laminara:session:" + id.String()
}

func (s *SessionStore) Create(ctx context.Context, session *auth.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, sessionKey(session.ID), data, s.ttl).Err()
}

func (s *SessionStore) Get(ctx context.Context, id uuid.UUID) (*auth.Session, error) {
	data, err := s.client.Get(ctx, sessionKey(id)).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, auth.ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	var session auth.Session
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}
	return &session, nil
}

func (s *SessionStore) Update(ctx context.Context, session *auth.Session) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	err = s.client.SetArgs(ctx, sessionKey(session.ID), data, redis.SetArgs{Mode: "XX", KeepTTL: true}).Err()
	if errors.Is(err, redis.Nil) {
		return auth.ErrSessionNotFound
	}
	return err
}

func (s *SessionStore) Revoke(ctx context.Context, id uuid.UUID) error {
	session, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	session.Revoked = true
	return s.Update(ctx, session)
}
