package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/hash"
	"github.com/laminara/laminara/server/internal/auth/totp"
)

func init() {
	auth.RegisterProvider("jsonfile", newJSONFile)
}

type jsonFileConfig struct {
	Path   string `json:"path"`
	Hash   string `json:"hash"`
	Fields struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		UUID      string `json:"uuid"`
		TwoFactor string `json:"twoFactorSecret"`
	} `json:"fields"`
}

type jsonFileProvider struct {
	verifier    hash.Verifier
	scheme      string
	usernameKey string
	passwordKey string
	uuidKey     string
	secretKey   string
	users       map[string]map[string]any
	second      *totp.Verifier
}

func newJSONFile(raw json.RawMessage) (auth.Provider, error) {
	var cfg jsonFileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	scheme := orDefault(cfg.Hash, "argon2id")
	verifier, err := verifierFor(scheme)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(cfg.Path)
	if err != nil {
		return nil, err
	}
	var records []map[string]any
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	usernameKey := orDefault(cfg.Fields.Username, "username")
	secretKey := orDefault(cfg.Fields.TwoFactor, "totp")
	users := make(map[string]map[string]any, len(records))
	for _, record := range records {
		name, _ := record[usernameKey].(string)
		if raw, present := record[secretKey]; present && raw != nil {
			secret, isString := raw.(string)
			if !isString || strings.TrimSpace(secret) == "" {
				return nil, fmt.Errorf("jsonfile auth: record %q carries %q that is not a base32 two-factor secret", name, secretKey)
			}
		}
		if name, ok := record[usernameKey].(string); ok {
			users[name] = record
		}
	}
	return &jsonFileProvider{
		verifier:    verifier,
		scheme:      scheme,
		usernameKey: usernameKey,
		passwordKey: orDefault(cfg.Fields.Password, "password"),
		uuidKey:     orDefault(cfg.Fields.UUID, "uuid"),
		secretKey:   secretKey,
		users:       users,
		second:      totp.NewVerifier(),
	}, nil
}

func (p *jsonFileProvider) Authenticate(_ context.Context, creds auth.Credentials) (auth.Identity, error) {
	record, ok := p.users[creds.Username]
	if !ok {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	stored, _ := record[p.passwordKey].(string)
	valid, err := verify(p.verifier, p.scheme, creds.Password, stored)
	if err != nil {
		return auth.Identity{}, err
	}
	if !valid {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if secret, _ := record[p.secretKey].(string); strings.TrimSpace(secret) != "" {
		if creds.TwoFactorCode == "" {
			return auth.Identity{}, auth.ErrTwoFactorRequired
		}
		if !p.second.Verify(creds.Username, secret, creds.TwoFactorCode) {
			return auth.Identity{}, auth.ErrInvalidCredentials
		}
	}
	id := auth.OfflineUUID(creds.Username)
	if raw, ok := record[p.uuidKey].(string); ok {
		if parsed, err := uuid.Parse(raw); err == nil {
			id = parsed
		}
	}
	return auth.Identity{Subject: creds.Username, Username: creds.Username, UUID: id}, nil
}

func orDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
