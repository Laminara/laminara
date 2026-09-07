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
	var document skinDocument
	if err := httpx.GetJSON(ctx, p.http, substitute(p.url, username, uuid), &document); err != nil {
		return Textures{}, err
	}
	return document.textures(), nil
}
