package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
)

func init() {
	auth.RegisterProvider("http", newHTTP)
}

type httpConfig struct {
	URL           string `json:"url"`
	UsernameField string `json:"usernameField"`
	PasswordField string `json:"passwordField"`
	UUIDField     string `json:"uuidField"`
	SuccessField  string `json:"successField"`
}

type httpProvider struct {
	client        *http.Client
	url           string
	usernameField string
	passwordField string
	uuidField     string
	successField  string
}

func newHTTP(raw json.RawMessage) (auth.Provider, error) {
	var cfg httpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, errors.New("http auth provider requires a url")
	}
	return &httpProvider{
		client:        &http.Client{Timeout: 10 * time.Second},
		url:           cfg.URL,
		usernameField: orDefault(cfg.UsernameField, "username"),
		passwordField: orDefault(cfg.PasswordField, "password"),
		uuidField:     cfg.UUIDField,
		successField:  cfg.SuccessField,
	}, nil
}

func (p *httpProvider) Authenticate(ctx context.Context, creds auth.Credentials) (auth.Identity, error) {
	body, err := json.Marshal(map[string]string{p.usernameField: creds.Username, p.passwordField: creds.Password})
	if err != nil {
		return auth.Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return auth.Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return auth.Identity{}, err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized, resp.StatusCode == http.StatusForbidden:
		return auth.Identity{}, auth.ErrInvalidCredentials
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		return auth.Identity{}, fmt.Errorf("auth endpoint returned status %d", resp.StatusCode)
	}

	var payload map[string]any
	if data, _ := io.ReadAll(resp.Body); len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}
	if p.successField != "" && !truthy(payload[p.successField]) {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	id := auth.OfflineUUID(creds.Username)
	if raw, ok := payload[p.uuidField].(string); ok {
		if parsed, err := uuid.Parse(raw); err == nil {
			id = parsed
		}
	}
	return auth.Identity{Subject: creds.Username, Username: creds.Username, UUID: id}, nil
}

func truthy(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1" || v == "ok"
	case float64:
		return v != 0
	default:
		return value != nil
	}
}
