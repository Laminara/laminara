package doctor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/serversetup"
	"github.com/laminara/laminara/server/internal/skin"
)

type servedElsewhere struct{ url string }

func (s servedElsewhere) Textures(context.Context, string, string) (skin.Textures, error) {
	return skin.Textures{SkinURL: s.url}, nil
}

func skinOptions(t *testing.T, sourceURL string, domains []string, provider skin.Provider) Options {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"url": sourceURL})
	if err != nil {
		t.Fatal(err)
	}
	return Options{
		Config: &config.Config{Yggdrasil: &config.YggdrasilConfig{
			Enabled:     true,
			SkinConfig:  raw,
			SkinDomains: domains,
		}},
		Wired: &serversetup.Wired{Skins: provider},
	}
}

func report(t *testing.T, opts Options) (diag.Verdict, string) {
	t.Helper()
	probe := diag.New("скины")
	checkSkinDomains(context.Background(), opts, probe)
	results := probe.Results()
	if len(results) == 0 {
		t.Fatal("проверка ничего не сказала")
	}
	return results[0].Verdict, results[0].Detail + " " + results[0].Remedy.Hint
}

func TestATextureHostFromTheAnswerIsCheckedToo(t *testing.T) {
	opts := skinOptions(t, "https://api.example.com/skins/%uuid%", []string{"api.example.com"},
		servedElsewhere{url: "http://cdn.example.net/skins/Steve.png"})

	level, text := report(t, opts)
	if level != diag.Fail {
		t.Fatalf("домен из ответа источника не проверили: %v — %s", level, text)
	}
	if !strings.Contains(text, "cdn.example.net") {
		t.Fatalf("в отчёте нет адреса, по которому игра пойдёт за скином: %s", text)
	}
	if !strings.Contains(text, "в ответе источника") {
		t.Fatalf("оператор не поймёт, откуда взялся этот адрес: %s", text)
	}
}

func TestBothHostsAllowedIsFine(t *testing.T) {
	opts := skinOptions(t, "https://api.example.com/skins/%uuid%",
		[]string{"api.example.com", "cdn.example.net"},
		servedElsewhere{url: "http://cdn.example.net/skins/Steve.png"})

	level, text := report(t, opts)
	if level != diag.OK {
		t.Fatalf("оба домена разрешены, а проверка недовольна: %v — %s", level, text)
	}
}

func TestAnIPWithAPortIsMatchedByItsAddress(t *testing.T) {
	opts := skinOptions(t, "http://31.77.10.155:3000/skins/profile/%uuid%", []string{"31.77.10.155"},
		servedElsewhere{url: "http://31.77.10.155:3000/skins/Steve"})

	level, text := report(t, opts)
	if level != diag.OK {
		t.Fatalf("адрес с портом должен покрываться записью без порта: %v — %s", level, text)
	}
}
