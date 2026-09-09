package skin

import (
	"context"
	"encoding/json"
)

func init() {
	Register("template", newTemplate)
}

type templateConfig struct {
	Skin string `json:"skin"`
	Cape string `json:"cape"`
	Slim bool   `json:"slim"`
}

type templateProvider struct {
	skin string
	cape string
	slim bool
}

func newTemplate(raw json.RawMessage) (Provider, error) {
	var cfg templateConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	return &templateProvider{skin: cfg.Skin, cape: cfg.Cape, slim: cfg.Slim}, nil
}

func (p *templateProvider) Textures(_ context.Context, username, uuid string) (Textures, error) {
	if p.skin == "" {
		return Textures{Slim: p.slim}, nil
	}
	textures := Textures{SkinURL: substitute(p.skin, username, uuid), Slim: p.slim}
	if p.cape != "" {
		textures.CapeURL = substitute(p.cape, username, uuid)
	}
	return textures, nil
}
