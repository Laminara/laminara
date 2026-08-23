package skin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

func init() {
	Register("json", newJSON)
}

type jsonConfig struct {
	URL string `json:"url"`
}

type jsonProvider struct {
	http *http.Client
	url  string
}

func newJSON(raw json.RawMessage) (Provider, error) {
	var cfg jsonConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.URL == "" {
		return nil, errors.New("json skin provider requires a url")
	}
	return &jsonProvider{http: &http.Client{Timeout: 10 * time.Second}, url: cfg.URL}, nil
}

type skinDocument struct {
	Skin  string `json:"skin"`
	Cape  string `json:"cape"`
	Model string `json:"model"`
}

func (p *jsonProvider) Textures(ctx context.Context, username, uuid string) (Textures, error) {
	var document skinDocument
	if err := httpx.GetJSON(ctx, p.http, substitute(p.url, username, uuid), &document); err != nil {
		return Textures{}, err
	}
	return Textures{SkinURL: document.Skin, CapeURL: document.Cape, Slim: document.Model == "slim"}, nil
}
