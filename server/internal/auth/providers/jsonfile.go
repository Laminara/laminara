package providers

import (
	"context"
	"encoding/json"
	"os"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/auth/hash"
)

func init() {
	auth.RegisterProvider("jsonfile", newJSONFile)
}

type jsonFileConfig struct {
	Path   string `json:"path"`
	Hash   string `json:"hash"`
	Fields struct {
		Username string `json:"username"`
		Password string `json:"password"`
		UUID     string `json:"uuid"`
	} `json:"fields"`
}

type jsonFileProvider struct {
	verifier    hash.Verifier
	usernameKey string
	passwordKey string
	uuidKey     string
	users       map[string]map[string]any
}

func newJSONFile(raw json.RawMessage) (auth.Provider, error) {
	var cfg jsonFileConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	verifier, err := hash.Get(orDefault(cfg.Hash, "argon2id"))
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
	users := make(map[string]map[string]any, len(records))
	for _, record := range records {
		if name, ok := record[usernameKey].(string); ok {
			users[name] = record
		}
	}
	return &jsonFileProvider{
		verifier:    verifier,
		usernameKey: usernameKey,
		passwordKey: orDefault(cfg.Fields.Password, "password"),
		uuidKey:     orDefault(cfg.Fields.UUID, "uuid"),
		users:       users,
	}, nil
}

func (p *jsonFileProvider) Authenticate(_ context.Context, creds auth.Credentials) (auth.Identity, error) {
	record, ok := p.users[creds.Username]
	if !ok {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	stored, _ := record[p.passwordKey].(string)
	valid, err := p.verifier.Verify(creds.Password, stored)
	if err != nil {
		return auth.Identity{}, err
	}
	if !valid {
		return auth.Identity{}, auth.ErrInvalidCredentials
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
