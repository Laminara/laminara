package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/laminara/laminara/server/internal/auth"
)

func init() {
	auth.RegisterProvider("http", newHTTP)
}

type httpConfig struct {
	URL               string `json:"url"`
	UsernameField     string `json:"usernameField"`
	PasswordField     string `json:"passwordField"`
	CodeField         string `json:"codeField"`
	UUIDField         string `json:"uuidField"`
	SuccessField      string `json:"successField"`
	SecondFactorField string `json:"secondFactorField"`
}

type httpProvider struct {
	client            *http.Client
	url               string
	usernameField     string
	passwordField     string
	codeField         string
	uuidField         string
	successField      string
	secondFactorField string
}

func newHTTP(raw json.RawMessage) (auth.Provider, error) {
	var cfg httpConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, errors.New("http auth provider requires a url")
	}
	warnIfInsecure(cfg.URL)
	return &httpProvider{
		client:            &http.Client{Timeout: 10 * time.Second},
		url:               cfg.URL,
		usernameField:     orDefault(cfg.UsernameField, "username"),
		passwordField:     orDefault(cfg.PasswordField, "password"),
		codeField:         orDefault(cfg.CodeField, "totp"),
		uuidField:         cfg.UUIDField,
		successField:      cfg.SuccessField,
		secondFactorField: cfg.SecondFactorField,
	}, nil
}

func warnIfInsecure(raw string) {
	if !sendsPasswordsInClear(raw) {
		return
	}
	slog.Warn("пароли игроков уходят провайдеру по незашифрованному http",
		"source", "auth",
		"url", raw,
		"почему", "логин и пароль видны любому между сервером и вашим API",
		"что делать", "поднимите TLS и укажите https, либо держите этот API на localhost за общим прокси",
	)
}

func sendsPasswordsInClear(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "http" {
		return false
	}
	return !isLoopbackHost(parsed.Hostname())
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func (p *httpProvider) Authenticate(ctx context.Context, creds auth.Credentials) (auth.Identity, error) {
	body := map[string]string{p.usernameField: creds.Username, p.passwordField: creds.Password}
	if creds.TwoFactorCode != "" {
		body[p.codeField] = creds.TwoFactorCode
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return auth.Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.url, bytes.NewReader(encoded))
	if err != nil {
		return auth.Identity{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return auth.Identity{}, err
	}
	defer resp.Body.Close()

	var payload map[string]any
	if data, _ := io.ReadAll(resp.Body); len(data) > 0 {
		_ = json.Unmarshal(data, &payload)
	}

	accepted := resp.StatusCode >= 200 && resp.StatusCode < 300
	if accepted && p.successField != "" {
		accepted = marked(lookup(payload, p.successField))
	}
	if accepted {
		id := auth.OfflineUUID(creds.Username)
		if raw, ok := lookup(payload, p.uuidField).(string); ok {
			if parsed, err := uuid.Parse(raw); err == nil {
				id = parsed
			}
		}
		return auth.Identity{Subject: creds.Username, Username: creds.Username, UUID: id}, nil
	}

	if p.secondFactorField != "" && marked(lookup(payload, p.secondFactorField)) {
		return auth.Identity{}, auth.ErrTwoFactorRequired
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden || p.successField != "" {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	return auth.Identity{}, fmt.Errorf("auth endpoint returned status %d", resp.StatusCode)
}

func lookup(payload map[string]any, path string) any {
	if path == "" {
		return nil
	}
	var current any = payload
	for _, part := range strings.Split(path, ".") {
		object, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current, ok = object[part]
		if !ok {
			return nil
		}
	}
	return current
}

func marked(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return v != "" && v != "false" && v != "0"
	case float64:
		return v != 0
	case nil:
		return false
	default:
		return true
	}
}
