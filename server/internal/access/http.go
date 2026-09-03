package access

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

func init() {
	RegisterSource("http", newHTTPSource)
}

type httpSourceConfig struct {
	URL      string            `json:"url"`
	Mode     string            `json:"mode"`
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"`
	Timeout  string            `json:"timeout"`
	CacheTTL string            `json:"cacheTTL"`
	FailOpen bool              `json:"failOpen"`
}

type httpSource struct {
	url      string
	check    bool
	method   string
	headers  map[string]string
	ttl      time.Duration
	failOpen bool
	client   *http.Client

	mu      sync.Mutex
	entries map[string]*httpEntry
}

type httpEntry struct {
	once    sync.Once
	ready   chan struct{}
	fetched time.Time
	roster  *Roster
	allowed bool
	err     error
}

const (
	defaultHTTPTimeout  = 5 * time.Second
	defaultHTTPCacheTTL = time.Minute
	errorCacheTTL       = 10 * time.Second
	maxCachedEntries    = 50_000
	maxRosterBytes      = 8 << 20
)

func parseOptionalDuration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

func newHTTPSource(config json.RawMessage) (Source, error) {
	var cfg httpSourceConfig
	if len(config) > 0 {
		if err := json.Unmarshal(config, &cfg); err != nil {
			return nil, err
		}
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("http access source needs a url")
	}
	if _, err := url.Parse(cfg.URL); err != nil {
		return nil, fmt.Errorf("http access source url: %w", err)
	}
	mode := strings.ToLower(cfg.Mode)
	if mode == "" {
		mode = "list"
	}
	if mode != "list" && mode != "check" {
		return nil, fmt.Errorf("http access source mode %q (want list or check)", cfg.Mode)
	}
	timeout, err := parseOptionalDuration(cfg.Timeout, defaultHTTPTimeout)
	if err != nil {
		return nil, fmt.Errorf("http access source timeout: %w", err)
	}
	ttl, err := parseOptionalDuration(cfg.CacheTTL, defaultHTTPCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("http access source cacheTTL: %w", err)
	}
	method := strings.ToUpper(cfg.Method)
	if method == "" {
		method = http.MethodGet
	}
	return &httpSource{
		url:      cfg.URL,
		check:    mode == "check",
		method:   method,
		headers:  cfg.Headers,
		ttl:      ttl,
		failOpen: cfg.FailOpen,
		client:   &http.Client{Timeout: timeout},
		entries:  map[string]*httpEntry{},
	}, nil
}

func (h *httpSource) endpoint(build string, subject Subject) string {
	query := url.Values{"build": {build}}
	if h.check {
		query.Set("username", subject.Username)
		query.Set("uuid", subject.UUID)
		query.Set("subject", subject.Subject)
	}
	separator := "?"
	if strings.Contains(h.url, "?") {
		separator = "&"
	}
	return h.url + separator + query.Encode()
}

func (h *httpSource) live(entry *httpEntry) bool {
	ttl := h.ttl
	if entry.err != nil {
		ttl = min(h.ttl, errorCacheTTL)
	}
	return time.Since(entry.fetched) < ttl
}

func (h *httpSource) entryFor(key string) (*httpEntry, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if entry, ok := h.entries[key]; ok {
		select {
		case <-entry.ready:
			if h.live(entry) {
				return entry, true
			}
		default:
			return entry, true
		}
	}
	if len(h.entries) >= maxCachedEntries {
		h.evictLocked()
	}
	entry := &httpEntry{ready: make(chan struct{})}
	h.entries[key] = entry
	return entry, false
}

func (h *httpSource) evictLocked() {
	settled := func(entry *httpEntry) bool {
		select {
		case <-entry.ready:
			return true
		default:
			return false
		}
	}
	for key, entry := range h.entries {
		if settled(entry) && !h.live(entry) {
			delete(h.entries, key)
		}
	}
	if len(h.entries) < maxCachedEntries {
		return
	}
	for key, entry := range h.entries {
		if settled(entry) {
			delete(h.entries, key)
		}
	}
}

func (h *httpSource) Allows(ctx context.Context, build string, subject Subject) (bool, error) {
	key := build
	if h.check {
		key = build + "\x00" + subject.Subject + "\x00" + subject.Username + "\x00" + subject.UUID
	}
	entry, cached := h.entryFor(key)
	if !cached {
		h.fill(ctx, entry, build, subject)
	}
	select {
	case <-entry.ready:
	case <-ctx.Done():
		if h.failOpen {
			slog.Warn("источник доступа не успел ответить — пускаю по failOpen",
				"source", "access",
				"сборка", build,
				"игрок", subject.Username,
			)
		}
		return h.failOpen, ctx.Err()
	}
	if entry.err != nil {
		if h.failOpen {
			slog.Warn("источник доступа не ответил — пускаю по failOpen",
				"source", "access",
				"сборка", build,
				"игрок", subject.Username,
				"error", entry.err,
			)
			return true, nil
		}
		return false, entry.err
	}
	if h.check {
		return entry.allowed, nil
	}
	return entry.roster.Contains(build, subject), nil
}

func (h *httpSource) fill(ctx context.Context, entry *httpEntry, build string, subject Subject) {
	entry.once.Do(func() {
		defer close(entry.ready)
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.client.Timeout)
		defer cancel()
		roster, allowed, err := h.fetch(fetchCtx, build, subject)
		entry.roster, entry.allowed, entry.err = roster, allowed, err
		entry.fetched = time.Now()
	})
}

func (h *httpSource) fetch(ctx context.Context, build string, subject Subject) (*Roster, bool, error) {
	request, err := http.NewRequestWithContext(ctx, h.method, h.endpoint(build, subject), nil)
	if err != nil {
		return nil, false, err
	}
	request.Header.Set("Accept", "application/json")
	for name, value := range h.headers {
		request.Header.Set(name, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		return nil, false, err
	}
	defer response.Body.Close()

	if h.check && (response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusNotFound) {
		return nil, false, nil
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, false, fmt.Errorf("access endpoint returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxRosterBytes))
	if err != nil {
		return nil, false, err
	}
	if h.check {
		allowed, err := parseCheckResponse(body)
		return nil, allowed, err
	}
	roster, err := ParseRoster(body)
	return roster, false, err
}

type checkResponse struct {
	Allowed *bool `json:"allowed"`
}

func parseCheckResponse(body []byte) (bool, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return false, fmt.Errorf("access endpoint answered with an empty body, want {\"allowed\": bool}")
	}
	var parsed checkResponse
	if err := json.Unmarshal([]byte(trimmed), &parsed); err != nil {
		return false, fmt.Errorf("access endpoint returned %q, want {\"allowed\": bool}", trimmed)
	}
	if parsed.Allowed == nil {
		return false, fmt.Errorf("access endpoint response has no \"allowed\" field")
	}
	return *parsed.Allowed, nil
}

func (h *httpSource) Reload() {
	h.mu.Lock()
	h.entries = map[string]*httpEntry{}
	h.mu.Unlock()
}
