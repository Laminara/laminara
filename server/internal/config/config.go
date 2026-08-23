package config

import (
	"encoding/json"
	"os"
	"time"

	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/ratelimit"
)

type Config struct {
	Auth      *AuthConfig       `json:"auth"`
	Storage   *StorageConfig    `json:"storage"`
	Build     *BuildConfig      `json:"build"`
	API       *APIConfig        `json:"api"`
	Yggdrasil *YggdrasilConfig  `json:"yggdrasil"`
	Modules   *ModulesConfig    `json:"modules"`
	Launcher  *LauncherConfig   `json:"launcher"`
	Branding  *BrandingConfig   `json:"branding"`
	Access    *access.Config    `json:"access"`
	HWID      *hwid.Config      `json:"hwid"`
	RateLimit *ratelimit.Config `json:"rateLimit"`
	News      *news.Config      `json:"news"`
}

type BrandingConfig struct {
	Name            string `json:"name"`
	WindowTitle     string `json:"windowTitle"`
	Tagline         string `json:"tagline"`
	PrimaryColor    string `json:"primaryColor"`
	PrimaryInk      string `json:"primaryInk"`
	BackgroundColor string `json:"backgroundColor"`
	LogoPath        string `json:"logoPath"`
	HeroMediaPath   string `json:"heroMediaPath"`
	SiteURL         string `json:"siteUrl"`
}

type LauncherConfig struct {
	Dir string `json:"dir"`
}

type ModulesConfig struct {
	Dir    string                     `json:"dir"`
	Config map[string]json.RawMessage `json:"config"`
}

type StorageConfig struct {
	Backend string          `json:"backend"`
	Config  json.RawMessage `json:"config"`
}

type BuildConfig struct {
	ProfilesDir        string   `json:"profilesDir"`
	SigningKeyPath     string   `json:"signingKeyPath"`
	TrustedSigningKeys []string `json:"trustedSigningKeys"`
}

type APIConfig struct {
	Addr   string `json:"addr"`
	XAccel bool   `json:"xAccel"`
}

type YggdrasilConfig struct {
	Enabled      bool            `json:"enabled"`
	ServerName   string          `json:"serverName"`
	RSAKeyPath   string          `json:"rsaKeyPath"`
	SkinProvider string          `json:"skinProvider"`
	SkinConfig   json.RawMessage `json:"skinConfig"`
	SkinDomains  []string        `json:"skinDomains"`
}

type AuthConfig struct {
	Provider   string          `json:"provider"`
	Config     json.RawMessage `json:"config"`
	AccessTTL  Duration        `json:"accessTTL"`
	RefreshTTL Duration        `json:"refreshTTL"`
	Sessions   SessionConfig   `json:"sessions"`
}

type SessionConfig struct {
	Backend   string `json:"backend"`
	RedisAddr string `json:"redisAddr"`
}

type Duration time.Duration

func (d Duration) Duration() time.Duration { return time.Duration(d) }

func (d *Duration) UnmarshalJSON(data []byte) error {
	var value string
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return err
	}
	*d = Duration(parsed)
	return nil
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
