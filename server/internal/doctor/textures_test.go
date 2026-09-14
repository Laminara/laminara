package doctor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
)

func profileResponse(t *testing.T, skinURL string) *http.Response {
	t.Helper()
	body := map[string]any{"properties": []any{}}
	if skinURL != "" {
		payload, err := json.Marshal(map[string]any{
			"textures": map[string]any{"SKIN": map[string]string{"url": skinURL}},
		})
		if err != nil {
			t.Fatal(err)
		}
		body["properties"] = []any{map[string]string{
			"name":  "textures",
			"value": base64.StdEncoding.EncodeToString(payload),
		}}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{Body: io.NopCloser(strings.NewReader(string(raw)))}
}

func texturesReport(t *testing.T, skinURL string, domains []string) (diag.Verdict, string) {
	t.Helper()
	probe := diag.New("игрок")
	opts := Options{Config: &config.Config{Yggdrasil: &config.YggdrasilConfig{SkinDomains: domains}}}
	checkServedTextures(context.Background(), opts, probe, profileResponse(t, skinURL))
	results := probe.Results()
	if len(results) == 0 {
		t.Fatal("проверка промолчала")
	}
	return results[0].Verdict, results[0].Detail + " " + results[0].Remedy.Hint
}

func TestAProfileWithoutTexturesIsReported(t *testing.T) {
	verdict, text := texturesReport(t, "", []string{"cdn.example.com"})
	if verdict == diag.OK {
		t.Fatalf("профиль без текстур — это стандартный Стив у игрока: %s", text)
	}
	if !strings.Contains(text, "Стивом") {
		t.Fatalf("оператор не поймёт, что увидит игрок: %s", text)
	}
}

func TestASkinFromAForbiddenDomainIsReported(t *testing.T) {
	verdict, text := texturesReport(t, "http://31.77.10.155:3000/skins/Steve", []string{"minotar.net"})
	if verdict != diag.Fail {
		t.Fatalf("домен не в списке — игра молча не покажет скин: %v %s", verdict, text)
	}
	if !strings.Contains(text, "31.77.10.155") {
		t.Fatalf("в отчёте нет адреса, за которым пойдёт игра: %s", text)
	}
}

func TestAnAllowedSkinPassesTheDomainCheck(t *testing.T) {
	verdict, text := texturesReport(t, "https://cdn.example.com/Steve.png", []string{"cdn.example.com"})
	if verdict != diag.OK {
		t.Fatalf("домен разрешён, претензий быть не должно: %v %s", verdict, text)
	}
}
