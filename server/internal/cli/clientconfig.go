package cli

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/signing"
)

type clientEndpoint struct {
	ID      string `json:"id"`
	BaseURL string `json:"baseUrl"`
}

type clientConfigOut struct {
	Endpoints            []clientEndpoint `json:"endpoints"`
	ServerPublicKeyHex   string           `json:"serverPublicKeyHex"`
	TrustedPublicKeysHex []string         `json:"trustedPublicKeysHex,omitempty"`
	HWIDSaltHex          string           `json:"hwidSaltHex,omitempty"`
	Branding             *clientBranding  `json:"branding,omitempty"`
}

type clientBranding struct {
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

const maxInlineAsset = 8 << 20

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
	return "data:" + mimeOf(path) + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

func mimeOf(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".mp4":
		return "video/mp4"
	case ".webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}

func brandingFrom(cfg *config.Config) (*clientBranding, error) {
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
	return &clientBranding{
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

func clientConfigCmd() *cobra.Command {
	var configPath string
	var endpoints []string
	cmd := &cobra.Command{
		Use:   "client-config",
		Short: "напечатать конфигурацию, которую запекают в лаунчер: адреса, ключи подписи, оформление",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if cfg.Build == nil || cfg.Build.SigningKeyPath == "" {
				return fmt.Errorf("в конфиге нет build.signingKeyPath — без ключа подписи лаунчер собрать нельзя")
			}
			ring, err := signing.NewKeyring(cfg.Build.SigningKeyPath, cfg.Build.TrustedSigningKeys)
			if err != nil {
				return err
			}
			branding, err := brandingFrom(cfg)
			if err != nil {
				return err
			}
			salt, err := hwidSalt(cfg)
			if err != nil {
				return err
			}
			out := clientConfigOut{
				ServerPublicKeyHex:   ring.ActiveHex(),
				TrustedPublicKeysHex: ring.TrustedHex(),
				HWIDSaltHex:          salt,
				Branding:             branding,
			}
			if len(endpoints) == 0 {
				if cfg.API == nil || cfg.API.Addr == "" {
					return fmt.Errorf("не указан --endpoint, и в конфиге нет api.addr")
				}
				endpoints = []string{"http://" + cfg.API.Addr}
				fmt.Fprintln(os.Stderr, "Внимание: адрес не указан — беру api.addr, а это почти наверняка не тот адрес, по которому придут игроки. Укажите --endpoint")
			}
			for _, endpoint := range endpoints {
				out.Endpoints = append(out.Endpoints, clientEndpoint{ID: endpointID(endpoint), BaseURL: endpoint})
			}
			data, err := json.MarshalIndent(out, "", "  ")
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().StringArrayVar(&endpoints, "endpoint", nil, "публичный адрес, по которому придут игроки (можно несколько раз, по порядку предпочтения)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func endpointID(raw string) string {
	if parsed, err := url.Parse(raw); err == nil && parsed.Host != "" {
		return strings.Split(parsed.Host, ":")[0]
	}
	return raw
}
