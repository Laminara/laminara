package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/webconsole"
)

func DataDir(cfg *Config, configPath string) string {
	if cfg != nil && cfg.Build != nil {
		if path := strings.TrimSpace(cfg.Build.SigningKeyPath); path != "" {
			return filepath.Dir(path)
		}
		if path := strings.TrimSpace(cfg.Build.ProfilesDir); path != "" {
			return filepath.Dir(path)
		}
	}
	if cfg != nil && cfg.Launcher != nil {
		if path := strings.TrimSpace(cfg.Launcher.Dir); path != "" {
			return filepath.Dir(path)
		}
	}
	if cfg != nil && cfg.Storage != nil && cfg.Storage.Backend == "fs" {
		var fs struct {
			Root string `json:"root"`
		}
		if json.Unmarshal(cfg.Storage.Config, &fs) == nil {
			if root := strings.TrimSpace(fs.Root); root != "" {
				return filepath.Dir(root)
			}
		}
	}
	if configPath != "" {
		return filepath.Dir(configPath)
	}
	return ""
}

func Derived(configPath string) map[string]string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var cfg Config
	if json.Unmarshal(data, &cfg) != nil {
		return nil
	}
	return withDefaults(&cfg, configPath)
}

func withDefaults(cfg *Config, configPath string) map[string]string {
	filled := map[string]string{}
	if cfg == nil {
		return filled
	}
	dir := DataDir(cfg, configPath)
	if dir == "" {
		return filled
	}
	at := func(name string) string { return filepath.Join(dir, name) }
	fill := func(path, value string, target *string) {
		*target = value
		filled[path] = value
	}

	if cfg.Build != nil && cfg.Build.ProfilesDir != "" && cfg.Build.SigningKeyPath == "" {
		fill("build.signingKeyPath", at("signing.key"), &cfg.Build.SigningKeyPath)
	}
	if cfg.HWID != nil && cfg.HWID.Mode != hwid.ModeOff {
		if cfg.HWID.SaltPath == "" {
			fill("hwid.saltPath", at("hwid.salt"), &cfg.HWID.SaltPath)
		}
		if cfg.HWID.TicketSecretPath == "" {
			fill("hwid.ticketSecretPath", at("hwid.ticket.key"), &cfg.HWID.TicketSecretPath)
		}
		if cfg.HWID.Store.Backend == "" {
			fill("hwid.store.backend", "sql", &cfg.HWID.Store.Backend)
			cfg.HWID.Store.Config = json.RawMessage(`{"driver":"sqlite","dsn":` + quoted(at("hwid.db")) + `}`)
		}
	}
	if cfg.Yggdrasil != nil && cfg.Yggdrasil.Enabled {
		if cfg.Yggdrasil.RSAKeyPath == "" {
			fill("yggdrasil.rsaKeyPath", at("yggdrasil-rsa.pem"), &cfg.Yggdrasil.RSAKeyPath)
		}
		if cfg.Yggdrasil.SkinProvider == "" {
			fill("yggdrasil.skinProvider", "template", &cfg.Yggdrasil.SkinProvider)
		}
	}
	if cfg.Console == nil {
		cfg.Console = &webconsole.Config{}
	}
	if cfg.Console.On() {
		if cfg.Console.StatePath == "" {
			fill("console.statePath", at("console-sessions.json"), &cfg.Console.StatePath)
		}
		if cfg.Console.PublicURL == "" && cfg.Launcher != nil && len(cfg.Launcher.Endpoints) > 0 {
			fill("console.publicUrl", cfg.Launcher.Endpoints[0], &cfg.Console.PublicURL)
		}
	}
	if cfg.Modules == nil {
		cfg.Modules = &ModulesConfig{}
	}
	if cfg.Modules.Dir == "" {
		fill("modules.dir", at("modules"), &cfg.Modules.Dir)
	}
	return filled
}

func quoted(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}
	return string(encoded)
}
