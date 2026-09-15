package serversetup

import (
	"net/url"
	"slices"
	"strings"

	"github.com/laminara/laminara/server/internal/config"
)

const TextureMirrorPath = "/yggdrasil/textures"

func TextureMirrorBase(cfg *config.Config) string {
	if cfg == nil || cfg.Yggdrasil == nil || !cfg.Yggdrasil.Enabled || !cfg.Yggdrasil.Mirrors() {
		return ""
	}
	if cfg.Launcher == nil || len(cfg.Launcher.Endpoints) == 0 {
		return ""
	}
	endpoint := strings.TrimSpace(cfg.Launcher.Endpoints[0])
	if endpoint == "" {
		return ""
	}
	return strings.TrimSuffix(endpoint, "/") + TextureMirrorPath
}

func TextureMirrorHost(cfg *config.Config) string {
	parsed, err := url.Parse(TextureMirrorBase(cfg))
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}

func TextureDomains(cfg *config.Config) []string {
	if cfg == nil || cfg.Yggdrasil == nil {
		return nil
	}
	domains := slices.Clone(cfg.Yggdrasil.SkinDomains)
	if host := TextureMirrorHost(cfg); host != "" && !slices.Contains(domains, host) {
		domains = append(domains, host)
	}
	return domains
}
