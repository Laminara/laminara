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

const maxAuthResponseBytes = 1 << 20

type httpConfig struct {
	LookupURL         string            `json:"lookupURL"`
	URL               string            `json:"url"`
	Headers           map[string]string `json:"headers"`
	Timeout           string            `json:"timeout"`
	UsernameField     string            `json:"usernameField"`
	PasswordField     string            `json:"passwordField"`
	CodeField         string            `json:"codeField"`
	NameField         string            `json:"nameField"`
	UUIDField         string            `json:"uuidField"`
	SuccessField      string            `json:"successField"`
	SecondFactorField string            `json:"secondFactorField"`
}

type httpProvider struct {
	client            *http.Client
	lookupURL         string
	url               string
	headers           map[string]string
	usernameField     string
	passwordField     string
	codeField         string
	nameField         string
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
	timeout := 10 * time.Second
	if cfg.Timeout != "" {
		parsed, err := time.ParseDuration(cfg.Timeout)
		if err != nil {
			return nil, fmt.Errorf("http auth provider timeout: %w", err)
		}
		timeout = parsed
	}
	return &httpProvider{
		client:            &http.Client{Timeout: timeout},
		url:               cfg.URL,
		lookupURL:         cfg.LookupURL,
		headers:           cfg.Headers,
		usernameField:     orDefault(cfg.UsernameField, "username"),
		passwordField:     orDefault(cfg.PasswordField, "password"),
		codeField:         orDefault(cfg.CodeField, "totp"),
		uuidField:         cfg.UUIDField,
		nameField:         cfg.NameField,
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
	resp, err := p.post(ctx, p.url, body)
	if err != nil {
		return auth.Identity{}, err
	}
	defer resp.Body.Close()

	var payload map[string]any
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAuthResponseBytes+1))
	if err != nil {
		return auth.Identity{}, fmt.Errorf("ответ источника аккаунтов не дочитался: %w", err)
	}
	if len(data) > maxAuthResponseBytes {
		return auth.Identity{}, fmt.Errorf("ответ источника аккаунтов больше допустимого размера")
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &payload); err != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return auth.Identity{}, fmt.Errorf("ответ источника аккаунтов не разобрать: %w", err)
		}
	}

	accepted := resp.StatusCode >= 200 && resp.StatusCode < 300
	if accepted && p.successField != "" {
		accepted = marked(lookup(payload, p.successField))
	}
	if accepted {
		if p.nameField != "" {
			name, ok := lookup(payload, p.nameField).(string)
			if !ok || name == "" {
				return auth.Identity{}, fmt.Errorf("в ответе источника аккаунтов нет имени игрока — проверьте nameField")
			}
			creds.Username = name
		}
		raw := ""
		if p.uuidField != "" {
			value, ok := lookup(payload, p.uuidField).(string)
			if !ok || value == "" {
				return auth.Identity{}, fmt.Errorf("в ответе источника аккаунтов нет UUID — проверьте uuidField")
			}
			raw = value
		}
		id, err := accountUUID(raw, creds.Username)
		if err != nil {
			return auth.Identity{}, err
		}
		return auth.Identity{Subject: creds.Username, Username: creds.Username, UUID: id}, nil
	}

	if p.secondFactorField != "" && marked(lookup(payload, p.secondFactorField)) {
		return auth.Identity{}, auth.ErrTwoFactorRequired
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	return auth.Identity{}, fmt.Errorf("источник аккаунтов ответил %d", resp.StatusCode)
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

func (p *httpProvider) LookupProfile(ctx context.Context, username string) (auth.Identity, error) {
	if p.lookupURL == "" {
		return auth.Identity{}, fmt.Errorf("для поиска профилей у HTTP-источника задайте lookupURL")
	}
	resp, err := p.post(ctx, p.lookupURL, map[string]string{p.usernameField: username})
	if err != nil {
		return auth.Identity{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	if resp.StatusCode != http.StatusOK {
		return auth.Identity{}, fmt.Errorf("источник профилей ответил %d", resp.StatusCode)
	}
	var profile struct {
		Name string `json:"name"`
		ID   string `json:"id"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxAuthResponseBytes+1))
	if err != nil {
		return auth.Identity{}, err
	}
	if len(data) > maxAuthResponseBytes {
		return auth.Identity{}, fmt.Errorf("ответ источника профилей больше допустимого размера")
	}
	if err := json.Unmarshal(data, &profile); err != nil {
		return auth.Identity{}, err
	}
	id, err := uuid.Parse(profile.ID)
	if err != nil || id == uuid.Nil || !strings.EqualFold(profile.Name, username) {
		return auth.Identity{}, fmt.Errorf("источник профилей вернул неверный UUID или имя игрока")
	}
	return auth.Identity{Subject: profile.Name, Username: profile.Name, UUID: id}, nil
}

func (p *httpProvider) post(ctx context.Context, endpoint string, body map[string]string) (*http.Response, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, err
	}
	for name, value := range p.headers {
		req.Header.Set(name, value)
	}
	req.Header.Set("Content-Type", "application/json")
	return p.client.Do(req)
}
