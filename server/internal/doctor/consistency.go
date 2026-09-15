package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/hwid"
	"github.com/laminara/laminara/server/internal/serversetup"
	"github.com/laminara/laminara/server/internal/skin"
)

func checkConsistency(ctx context.Context, opts Options, probe *diag.Probe) {
	checkSkinDomains(ctx, opts, probe)
	checkXAccel(opts, probe)
	checkMachineDurability(opts, probe)
	checkEndpointsMatchConsole(opts, probe)
}

func checkSkinDomains(ctx context.Context, opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Yggdrasil == nil || !cfg.Yggdrasil.Enabled {
		return
	}
	sources := skinHosts(cfg.Yggdrasil.SkinConfig)
	served := servedHosts(ctx, opts)
	mirror := serversetup.TextureMirrorHost(cfg)
	if mirror != "" && len(served) == 1 && served[0] == mirror {
		mirroredSkinDomains(cfg, sources, probe)
		return
	}
	hosts := slices.Clone(sources)
	for _, host := range served {
		if !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	if len(hosts) == 0 {
		return
	}
	allowed := serversetup.TextureDomains(cfg)
	missing := notAllowed(hosts, allowed)
	if len(missing) == 0 {
		probe.OK("домены скинов", "%s разрешены", strings.Join(hosts, ", "))
		return
	}
	hint := "игра не станет загружать скины с домена, которого нет в этом списке — игроки будут стандартными Стивами"
	for _, host := range missing {
		if slices.Contains(served, host) && !slices.Contains(sources, host) {
			hint += "; этот адрес приходит в ответе источника скинов, а не стоит в настройках — в yggdrasil.skinConfig его не видно"
			break
		}
	}
	probe.Fail("домены скинов", fmt.Sprintf("%s нет в yggdrasil.skinDomains", strings.Join(missing, ", ")), diag.Remedy{
		Hint:    hint,
		Command: addDomains(cfg, missing),
	})
}

func mirroredSkinDomains(cfg *config.Config, sources []string, probe *diag.Probe) {
	probe.OK("домены скинов", "картинки раздаёт сам сервер — доменов источника в списке не требуется")
	missing := notAllowed(sources, cfg.Yggdrasil.SkinDomains)
	if len(missing) == 0 {
		return
	}
	probe.Warn("запасной путь к скинам", fmt.Sprintf("%s нет в yggdrasil.skinDomains", strings.Join(missing, ", ")), diag.Remedy{
		Hint:    "пока зеркало работает, этот домен не нужен; но если картинка перестанет скачиваться, сервер отдаст игре прямую ссылку — и без домена в списке игрок останется Стивом",
		Command: addDomains(cfg, missing),
	})
}

func notAllowed(hosts, allowed []string) []string {
	var missing []string
	for _, host := range hosts {
		if !domainAllowed(host, allowed) {
			missing = append(missing, host)
		}
	}
	return missing
}

func addDomains(cfg *config.Config, missing []string) string {
	return fmt.Sprintf("laminara-server settings yggdrasil.skinDomains %s",
		strings.Join(append(slices.Clone(cfg.Yggdrasil.SkinDomains), missing...), ","))
}

func servedHosts(ctx context.Context, opts Options) []string {
	if opts.Wired == nil || opts.Wired.Skins == nil {
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	textures, err := opts.Wired.Skins.Textures(callCtx, skin.ProbeUsername, skin.ProbeUUID)
	if err != nil {
		return nil
	}
	var hosts []string
	for _, raw := range []string{textures.SkinURL, textures.CapeURL} {
		if host := hostOf(raw); host != "" && !slices.Contains(hosts, host) {
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func skinHosts(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var cfg struct {
		Skin string `json:"skin"`
		Cape string `json:"cape"`
		URL  string `json:"url"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	placeholders := strings.NewReplacer("%nickname%", "Steve", "%username%", "Steve", "%uuid%", "0")
	seen := map[string]bool{}
	var hosts []string
	for _, candidate := range []string{cfg.Skin, cfg.Cape, cfg.URL} {
		if candidate == "" {
			continue
		}
		parsed, err := url.Parse(placeholders.Replace(candidate))
		if err != nil || parsed.Hostname() == "" {
			continue
		}
		host := parsed.Hostname()
		if !seen[host] {
			seen[host] = true
			hosts = append(hosts, host)
		}
	}
	return hosts
}

func domainAllowed(host string, allowed []string) bool {
	for _, entry := range allowed {
		trimmed := strings.TrimPrefix(entry, ".")
		if host == trimmed || strings.HasSuffix(host, "."+trimmed) {
			return true
		}
	}
	return false
}

func checkXAccel(opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.API == nil || !cfg.API.XAccel {
		return
	}
	if cfg.Storage == nil || cfg.Storage.Backend == "fs" {
		return
	}
	probe.Warn("раздача файлов", fmt.Sprintf("api.xAccel включён при хранилище %s", cfg.Storage.Backend), diag.Remedy{
		Hint:    "передавать раздачу nginx умеет только файловое хранилище; с S3 сервер и так отдаёт ссылку напрямую, а настройка ничего не делает",
		Command: "laminara-server settings api.xAccel false",
	})
}

func checkMachineDurability(opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.HWID == nil || cfg.HWID.Mode != hwid.ModeEnforce {
		return
	}
	backend := cfg.HWID.Store.Backend
	if backend == "" || backend == "memory" {
		probe.Fail("хранение банов", "включён режим «не пускать нарушителей», а база компьютеров держится в памяти", diag.Remedy{
			Hint:    "после каждого перезапуска все баны по железу исчезнут, а забаненные вернутся в игру",
			Command: "laminara-server settings hwid.store.backend sql",
		})
		return
	}
	probe.OK("хранение банов", "%s — баны переживут перезапуск", backend)
}

func checkEndpointsMatchConsole(opts Options, probe *diag.Probe) {
	cfg := opts.Config
	if cfg.Console == nil || cfg.Console.PublicURL == "" || cfg.Launcher == nil || len(cfg.Launcher.Endpoints) == 0 {
		return
	}
	consoleHost := hostOf(cfg.Console.PublicURL)
	if consoleHost == "" {
		return
	}
	for _, endpoint := range cfg.Launcher.Endpoints {
		if hostOf(endpoint) == consoleHost {
			probe.OK("адреса сервера", "%s — один и тот же домен для лаунчера и консоли", consoleHost)
			return
		}
	}
	probe.Warn("адреса сервера", fmt.Sprintf("консоль на %s, а лаунчер обращается к %s", consoleHost, strings.Join(cfg.Launcher.Endpoints, ", ")), diag.Remedy{
		Hint: "обычно это один домен; проверьте, что оба адреса ведут на этот сервер",
	})
}

func hostOf(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Hostname()
}
