package auth

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

type Config struct {
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

func DefaultConfig() Config {
	return Config{AccessTTL: 15 * time.Minute, RefreshTTL: 30 * 24 * time.Hour}
}

type Tokens struct {
	Access         string
	AccessExpires  time.Time
	Refresh        string
	RefreshExpires time.Time
}

type Service struct {
	provider Provider
	sessions SessionStore
	cfg      Config
	now      func() time.Time
}

func NewService(provider Provider, sessions SessionStore, cfg Config) *Service {
	return &Service{provider: provider, sessions: sessions, cfg: cfg, now: time.Now}
}

func (s *Service) Verify(ctx context.Context, username, password string) (Identity, error) {
	return s.provider.Authenticate(ctx, Credentials{Username: username, Password: password})
}

func (s *Service) Login(ctx context.Context, username, password string) (*Tokens, error) {
	identity, err := s.provider.Authenticate(ctx, Credentials{Username: username, Password: password})
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}
	return s.issue(ctx, identity)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (*Tokens, error) {
	sessionID, secret, err := parseToken(refreshToken)
	if err != nil {
		return nil, ErrInvalidToken
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, ErrInvalidToken
	}
	now := s.now()
	if session.Revoked || now.After(session.ExpiresAt) {
		return nil, ErrInvalidToken
	}
	if !secretMatchesHash(secret, session.RefreshTokenHash) {
		_ = s.sessions.Revoke(ctx, sessionID)
		return nil, ErrInvalidToken
	}
	accessToken, accessHash, err := newToken(sessionID)
	if err != nil {
		return nil, err
	}
	refreshTokenNew, refreshHash, err := newToken(sessionID)
	if err != nil {
		return nil, err
	}
	session.AccessTokenHash = accessHash
	session.AccessExpiresAt = now.Add(s.cfg.AccessTTL)
	session.RefreshTokenHash = refreshHash
	if err := s.sessions.Update(ctx, session); err != nil {
		return nil, err
	}
	return &Tokens{
		Access:         accessToken,
		AccessExpires:  session.AccessExpiresAt,
		Refresh:        refreshTokenNew,
		RefreshExpires: session.ExpiresAt,
	}, nil
}

func (s *Service) ValidateAccess(ctx context.Context, accessToken string) (Identity, error) {
	sessionID, secret, err := parseToken(accessToken)
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return Identity{}, ErrInvalidToken
	}
	if session.Revoked || s.now().After(session.AccessExpiresAt) {
		return Identity{}, ErrInvalidToken
	}
	if !secretMatchesHash(secret, session.AccessTokenHash) {
		return Identity{}, ErrInvalidToken
	}
	return session.Identity, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	sessionID, _, err := parseToken(token)
	if err != nil {
		return ErrInvalidToken
	}
	return s.sessions.Revoke(ctx, sessionID)
}

func (s *Service) issue(ctx context.Context, identity Identity) (*Tokens, error) {
	now := s.now()
	sessionID := uuid.New()
	accessToken, accessHash, err := newToken(sessionID)
	if err != nil {
		return nil, err
	}
	refreshToken, refreshHash, err := newToken(sessionID)
	if err != nil {
		return nil, err
	}
	session := &Session{
		ID:               sessionID,
		Identity:         identity,
		AccessTokenHash:  accessHash,
		AccessExpiresAt:  now.Add(s.cfg.AccessTTL),
		RefreshTokenHash: refreshHash,
		ExpiresAt:        now.Add(s.cfg.RefreshTTL),
	}
	if err := s.sessions.Create(ctx, session); err != nil {
		return nil, err
	}
	return &Tokens{
		Access:         accessToken,
		AccessExpires:  session.AccessExpiresAt,
		Refresh:        refreshToken,
		RefreshExpires: session.ExpiresAt,
	}, nil
}
