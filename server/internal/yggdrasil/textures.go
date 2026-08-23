package yggdrasil

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"

	"github.com/laminara/laminara/server/internal/skin"
)

type property struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type texture struct {
	URL      string            `json:"url"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type texturePayload struct {
	Timestamp   int64              `json:"timestamp"`
	ProfileID   string             `json:"profileId"`
	ProfileName string             `json:"profileName"`
	Textures    map[string]texture `json:"textures"`
}

func (s *Server) texturesProperty(uuid, username string, textures skin.Textures) (property, error) {
	payload := texturePayload{
		Timestamp:   s.now().UnixMilli(),
		ProfileID:   uuid,
		ProfileName: username,
		Textures:    map[string]texture{},
	}
	if textures.SkinURL != "" {
		entry := texture{URL: textures.SkinURL}
		if textures.Slim {
			entry.Metadata = map[string]string{"model": "slim"}
		}
		payload.Textures["SKIN"] = entry
	}
	if textures.CapeURL != "" {
		payload.Textures["CAPE"] = texture{URL: textures.CapeURL}
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return property{}, err
	}
	value := base64.StdEncoding.EncodeToString(raw)
	digest := sha1.Sum([]byte(value))
	signature, err := rsa.SignPKCS1v15(rand.Reader, s.rsa, crypto.SHA1, digest[:])
	if err != nil {
		return property{}, err
	}
	return property{
		Name:      "textures",
		Value:     value,
		Signature: base64.StdEncoding.EncodeToString(signature),
	}, nil
}
