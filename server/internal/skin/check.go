package skin

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/laminara/laminara/server/internal/diag"
)

const ProbeUsername = "Steve"

var ProbeUUID = "8667ba71-b85a-4004-af54-457a9734eed7"

func (p *templateProvider) Check(ctx context.Context, probe *diag.Probe) {
	textures, err := p.Textures(ctx, ProbeUsername, ProbeUUID)
	if err != nil {
		probe.Fail("скины", fmt.Sprintf("шаблон не разворачивается: %v", err), diag.Remedy{
			Hint: "проверьте yggdrasil.skinConfig.skin",
		})
		return
	}
	reachable(ctx, probe, "скины", textures.SkinURL)
	if textures.CapeURL != "" {
		reachable(ctx, probe, "плащи", textures.CapeURL)
	}
}

func (p *jsonProvider) Check(ctx context.Context, probe *diag.Probe) {
	started := time.Now()
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	textures, err := p.Textures(callCtx, ProbeUsername, ProbeUUID)
	if err != nil {
		probe.Warn("скины", fmt.Sprintf("%s не отвечает: %v", p.url, err), diag.Remedy{
			Hint: "игроки будут выглядеть стандартным Стивом; проверьте адрес источника скинов",
		})
		return
	}
	if textures.SkinURL == "" {
		probe.OK("скины", "%s отвечает (для %s скина нет — это нормально)", p.url, ProbeUsername)
		return
	}
	probe.OK("скины", "%s отвечает за %s", p.url, time.Since(started).Round(time.Millisecond))
	reachable(ctx, probe, "файл скина", textures.SkinURL)
}

func (p *sqlProvider) Check(ctx context.Context, probe *diag.Probe) {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.db.PingContext(pingCtx); err != nil {
		probe.Fail("скины", fmt.Sprintf("база скинов не отвечает: %v", err), diag.Remedy{
			Hint: "проверьте yggdrasil.skinConfig.dsn",
		})
		return
	}
	if _, err := p.Textures(ctx, ProbeUsername, ProbeUUID); err != nil {
		probe.Fail("скины", fmt.Sprintf("запрос не выполняется: %v", err), diag.Remedy{
			Hint: "проверьте таблицу и колонки в yggdrasil.skinConfig",
		})
		return
	}
	probe.OK("скины", "база отвечает, запрос выполняется")
}

func reachable(ctx context.Context, probe *diag.Probe, what, raw string) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		probe.Warn(what, fmt.Sprintf("%s — это не полный адрес", raw), diag.Remedy{
			Hint: "игра загружает скины по абсолютной ссылке вида https://…",
		})
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(callCtx, http.MethodHead, raw, nil)
	if err != nil {
		return
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		probe.Warn(what, fmt.Sprintf("%s недоступен: %v", raw, err), diag.Remedy{
			Hint: "проверка идёт по нику Steve — если такого игрока нет, это нормально; иначе игроки останутся без скинов",
		})
		return
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		probe.OK(what, "%s отвечает (для %s скина нет — это нормально)", parsed.Host, ProbeUsername)
		return
	}
	if response.StatusCode >= 400 {
		probe.Warn(what, fmt.Sprintf("%s отвечает %s", parsed.Host, response.Status), diag.Remedy{
			Hint: "игра скачивает скины сама, без токена — адрес должен открываться без авторизации",
		})
		return
	}
	probe.OK(what, "%s отдаёт файлы", parsed.Host)
}
