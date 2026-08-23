package news

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

func init() {
	RegisterSource("file", newFileSource)
	RegisterSource("http", newHTTPSource)
}

type fileConfig struct {
	Path string `json:"path"`
}

type fileSource struct {
	path string

	mu    sync.Mutex
	items []Item
	stamp string
}

func newFileSource(raw json.RawMessage) (Source, error) {
	var cfg fileConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
	}
	if cfg.Path == "" {
		return nil, fmt.Errorf("file news source needs a path")
	}
	return &fileSource{path: cfg.Path}, nil
}

func (f *fileSource) Items(context.Context) ([]Item, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	info, err := os.Stat(f.path)
	if err != nil {
		return f.items, nil
	}
	stamp := fmt.Sprintf("%d:%d", info.ModTime().UnixNano(), info.Size())
	if f.items != nil && stamp == f.stamp {
		return f.items, nil
	}
	data, err := os.ReadFile(f.path)
	if err != nil {
		return f.items, nil
	}
	items, err := Parse(data)
	if err != nil {
		return f.items, err
	}
	f.items, f.stamp = items, stamp
	return items, nil
}

type httpConfig struct {
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Timeout  string            `json:"timeout"`
	CacheTTL string            `json:"cacheTTL"`
}

type httpSource struct {
	url     string
	headers map[string]string
	ttl     time.Duration
	client  *http.Client

	mu      sync.Mutex
	items   []Item
	fetched time.Time
}

const maxNewsBytes = 1 << 20

func newHTTPSource(raw json.RawMessage) (Source, error) {
	var cfg httpConfig
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
	}
	if cfg.URL == "" {
		return nil, fmt.Errorf("http news source needs a url")
	}
	timeout, err := duration(cfg.Timeout, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("http news source timeout: %w", err)
	}
	ttl, err := duration(cfg.CacheTTL, defaultCacheTTL)
	if err != nil {
		return nil, fmt.Errorf("http news source cacheTTL: %w", err)
	}
	return &httpSource{url: cfg.URL, headers: cfg.Headers, ttl: ttl, client: &http.Client{Timeout: timeout}}, nil
}

func duration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

func (h *httpSource) Items(ctx context.Context) ([]Item, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.items != nil && time.Since(h.fetched) < h.ttl {
		return h.items, nil
	}
	h.fetched = time.Now()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, h.url, nil)
	if err != nil {
		return h.items, err
	}
	request.Header.Set("Accept", "application/json")
	for name, value := range h.headers {
		request.Header.Set(name, value)
	}
	response, err := h.client.Do(request)
	if err != nil {
		return h.items, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return h.items, fmt.Errorf("news endpoint returned %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxNewsBytes))
	if err != nil {
		return h.items, err
	}
	items, err := Parse(body)
	if err != nil {
		return h.items, err
	}
	h.items = items
	return items, nil
}
