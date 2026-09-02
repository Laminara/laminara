package clientconfig

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/mediatype"
	"github.com/laminara/laminara/server/internal/signing"
)

const maxInlineAsset = 8 << 20

type Endpoint struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
}

type Branding struct {
	Name             string `json:"name,omitempty"`
	WindowTitle      string `json:"windowTitle,omitempty"`
	Tagline          string `json:"tagline,omitempty"`
	PrimaryColor     string `json:"primaryColor,omitempty"`
	PrimaryInk       string `json:"primaryInk,omitempty"`
	BackgroundColor  string `json:"backgroundColor,omitempty"`
	LogoDataURI      string `json:"logoDataUri,omitempty"`
	HeroMediaDataURI string `json:"heroMediaDataUri,omitempty"`
	SiteURL          string `json:"siteUrl,omitempty"`
}

type Document struct {
	Endpoints            []Endpoint `json:"endpoints"`
	ServerPublicKeyHex   string     `json:"serverPublicKeyHex"`
	TrustedPublicKeysHex []string   `json:"trustedPublicKeysHex,omitempty"`
	HWIDSaltHex          string     `json:"hwidSaltHex,omitempty"`
	Branding             *Branding  `json:"branding,omitempty"`
}

func Build(cfg *config.Config, endpoints []string) (Document, error) {
	if cfg.Build == nil || cfg.Build.SigningKeyPath == "" {
		return Document{}, fmt.Errorf("в конфиге нет build.signingKeyPath — без ключа подписи лаунчер собрать нельзя")
	}
	ring, err := signing.NewKeyring(cfg.Build.SigningKeyPath, cfg.Build.TrustedSigningKeys)
	if err != nil {
		return Document{}, err
	}
	branding, err := brandingFrom(cfg)
	if err != nil {
		return Document{}, err
	}
	salt, err := hwidSalt(cfg)
	if err != nil {
		return Document{}, err
	}
	document := Document{
		ServerPublicKeyHex:   ring.ActiveHex(),
		TrustedPublicKeysHex: ring.TrustedHex(),
		HWIDSaltHex:          salt,
		Branding:             branding,
	}
	for _, endpoint := range endpoints {
		document.Endpoints = append(document.Endpoints, Endpoint{ID: EndpointID(endpoint), BaseURL: endpoint})
	}
	return document, nil
}

func (d Document) JSON() ([]byte, error) {
	return json.MarshalIndent(d, "", "  ")
}

func (d Document) LauncherName() string {
	name := "Laminara"
	if d.Branding != nil {
		if d.Branding.WindowTitle != "" {
			name = d.Branding.WindowTitle
		} else if d.Branding.Name != "" {
			name = d.Branding.Name
		}
	}
	cleaned := strings.Map(func(r rune) rune {
		if strings.ContainsRune(`/\:*?"<>|`, r) {
			return -1
		}
		return r
	}, name)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return "Laminara"
	}
	return cleaned
}

func EndpointID(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return strings.Split(parsed.Host, ":")[0]
	}
	return raw
}

func brandingFrom(cfg *config.Config) (*Branding, error) {
	if cfg.Branding == nil {
		return nil, nil
	}
	logo, err := inlineAsset(cfg.Branding.LogoPath)
	if err != nil {
		return nil, err
	}
	hero, err := inlineAsset(cfg.Branding.HeroMediaPath)
	if err != nil {
		return nil, err
	}
	return &Branding{
		Name:             cfg.Branding.Name,
		WindowTitle:      cfg.Branding.WindowTitle,
		Tagline:          cfg.Branding.Tagline,
		PrimaryColor:     cfg.Branding.PrimaryColor,
		PrimaryInk:       cfg.Branding.PrimaryInk,
		BackgroundColor:  cfg.Branding.BackgroundColor,
		LogoDataURI:      logo,
		HeroMediaDataURI: hero,
		SiteURL:          cfg.Branding.SiteURL,
	}, nil
}

func hwidSalt(cfg *config.Config) (string, error) {
	if cfg.HWID == nil || cfg.HWID.Mode == hwid.ModeOff {
		return "", nil
	}
	salt, err := hwid.LoadOrCreateSalt(cfg.HWID.SaltPath)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(salt), nil
}

func inlineAsset(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("картинка оформления %s: %w", path, err)
	}
	if len(data) > maxInlineAsset {
		return "", fmt.Errorf("картинка %s весит %s — оставьте под %s", path, humanize.Bytes(uint64(len(data))), humanize.Bytes(uint64(maxInlineAsset)))
	}
	return "data:" + mediatype.Guess(path, "") + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}
