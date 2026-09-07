package skin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

func init() {
	Register("json", newJSON)
}

type jsonConfig struct {
	URL      string            `json:"url"`
	Headers  map[string]string `json:"headers"`
	Timeout  string            `json:"timeout"`
	CacheTTL string            `json:"cacheTTL"`
}

type jsonProvider struct {
	http    *http.Client
	url     string
	headers map[string]string
	ttl     time.Duration

	mu    sync.Mutex
	cache map[string]cachedTextures
}

type cachedTextures struct {
	textures Textures
	fetched  time.Time
}

func newJSON(raw json.RawMessage) (Provider, error) {
	var cfg jsonConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, errors.New("json skin provider requires a url")
	}
	timeout, err := parseDuration(cfg.Timeout, 10*time.Second)
	if err != nil {
		return nil, fmt.Errorf("json skin provider timeout: %w", err)
	}
	ttl, err := parseDuration(cfg.CacheTTL, time.Minute)
	if err != nil {
		return nil, fmt.Errorf("json skin provider cacheTTL: %w", err)
	}
	return &jsonProvider{
		http:    &http.Client{Timeout: timeout},
		url:     cfg.URL,
		headers: cfg.Headers,
		ttl:     ttl,
		cache:   map[string]cachedTextures{},
	}, nil
}

func parseDuration(value string, fallback time.Duration) (time.Duration, error) {
	if value == "" {
		return fallback, nil
	}
	return time.ParseDuration(value)
}

type textureRef struct {
	url   string
	model string
}

func (t *textureRef) UnmarshalJSON(data []byte) error {
	var direct string
	if err := json.Unmarshal(data, &direct); err == nil {
		t.url = direct
		return nil
	}
	var nested struct {
		URL      string `json:"url"`
		Metadata struct {
			Model string `json:"model"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(data, &nested); err != nil {
		return err
	}
	t.url = nested.URL
	t.model = nested.Metadata.Model
	return nil
}

type skinDocument struct {
	Skin      textureRef `json:"skin"`
	Cape      textureRef `json:"cape"`
	Model     string     `json:"model"`
	SkinUpper textureRef `json:"SKIN"`
	CapeUpper textureRef `json:"CAPE"`
}

func (d skinDocument) textures() Textures {
	skin, cape := d.Skin, d.Cape
	if skin.url == "" {
		skin = d.SkinUpper
	}
	if cape.url == "" {
		cape = d.CapeUpper
	}
	model := d.Model
	if skin.model != "" {
		model = skin.model
	}
	return Textures{SkinURL: skin.url, CapeURL: cape.url, Slim: model == "slim"}
}

func (p *jsonProvider) Textures(ctx context.Context, username, uuid string) (Textures, error) {
	target := substitute(p.url, username, uuid)
	if textures, ok := p.cached(target); ok {
		return textures, nil
	}
	var document skinDocument
	if err := httpx.GetJSONWithHeaders(ctx, p.http, target, p.headers, &document); err != nil {
		return Textures{}, err
	}
	textures := document.textures()
	p.remember(target, textures)
	return textures, nil
}

func (p *jsonProvider) cached(key string) (Textures, bool) {
	if p.ttl <= 0 {
		return Textures{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	entry, ok := p.cache[key]
	if !ok || time.Since(entry.fetched) > p.ttl {
		return Textures{}, false
	}
	return entry.textures, true
}

func (p *jsonProvider) remember(key string, textures Textures) {
	if p.ttl <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cache) > 4096 {
		p.cache = map[string]cachedTextures{}
	}
	p.cache[key] = cachedTextures{textures: textures, fetched: time.Now()}
}
