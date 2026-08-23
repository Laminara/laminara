package news_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/news"
)

func serviceFor(t *testing.T, raw string) *news.Service {
	t.Helper()
	var cfg news.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	service, err := news.New(&cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return service
}

func TestUnconfiguredServesNothing(t *testing.T) {
	service, err := news.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	if service.Enabled() {
		t.Fatal("news must be off until an operator configures a source")
	}
	if items := service.Latest(context.Background()); len(items) != 0 {
		t.Fatalf("expected no items, got %d", len(items))
	}
}

func TestFileSourceSortsNewestFirstAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "news.json")
	write := func(body string) {
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write(`[
		{"id": "old", "title": "Старая", "publishedAt": "2026-01-01T00:00:00Z"},
		{"id": "new", "title": "Свежая", "publishedAt": "2026-06-01T00:00:00Z"}
	]`)
	service := serviceFor(t, fmt.Sprintf(`{"source": {"type": "file", "config": {"path": %q}}}`, path))

	items := service.Latest(context.Background())
	if len(items) != 2 || items[0].Id != "new" {
		t.Fatalf("newest must come first, got %+v", items)
	}

	write(`{"items": [{"id": "only", "title": "Одна"}]}`)
	items = service.Latest(context.Background())
	if len(items) != 1 || items[0].Id != "only" {
		t.Fatalf("editing the file must take effect, got %+v", items)
	}
}

func TestHTTPSourceCachesAndSurvivesAnOutage(t *testing.T) {
	healthy := true
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if !healthy {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		fmt.Fprint(w, `[{"id": "a", "title": "Анонс"}]`)
	}))
	defer server.Close()

	service := serviceFor(t, fmt.Sprintf(`{"source": {"type": "http", "config": {"url": %q, "cacheTTL": "1h"}}}`, server.URL))
	for range 5 {
		if len(service.Latest(context.Background())) != 1 {
			t.Fatal("the endpoint served one item")
		}
	}
	if calls != 1 {
		t.Fatalf("the document must be fetched once per TTL, got %d fetches", calls)
	}

	healthy = false
	if len(service.Latest(context.Background())) != 1 {
		t.Fatal("a site that goes down must not empty the panel while the cache is warm")
	}
}

func TestOnlyWebLinksSurvive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "news.json")
	body := `[
		{"id": "web", "title": "Веб", "link": "https://example.com/x"},
		{"id": "file", "title": "Файл", "link": "file:///etc/passwd"},
		{"id": "script", "title": "Скрипт", "link": "javascript:alert(1)"},
		{"id": "steam", "title": "Схема", "link": "steam://run/1"}
	]`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	service := serviceFor(t, fmt.Sprintf(`{"source": {"type": "file", "config": {"path": %q}}}`, path))

	links := map[string]string{}
	for _, item := range service.Latest(context.Background()) {
		links[item.Id] = item.Link
	}
	if links["web"] != "https://example.com/x" {
		t.Fatalf("a plain https link must survive, got %q", links["web"])
	}
	for _, id := range []string{"file", "script", "steam"} {
		if links[id] != "" {
			t.Fatalf("%s link must be dropped, got %q", id, links[id])
		}
	}
}

func TestUntitledItemsAndOverlongBodiesAreTamed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "news.json")
	long := make([]rune, 5000)
	for i := range long {
		long[i] = 'я'
	}
	body := fmt.Sprintf(`[{"id": "a", "title": "  ", "body": "x"}, {"id": "b", "title": "Есть", "body": %q}]`, string(long))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	service := serviceFor(t, fmt.Sprintf(`{"source": {"type": "file", "config": {"path": %q}}}`, path))

	items := service.Latest(context.Background())
	if len(items) != 1 || items[0].Id != "b" {
		t.Fatalf("an item with no title has nothing to show, got %+v", items)
	}
	if runes := []rune(items[0].Body); len(runes) > 2001 {
		t.Fatalf("a runaway body must be clamped, got %d runes", len(runes))
	}
}

func TestUnknownSourceIsARefusalAtStartup(t *testing.T) {
	var cfg news.Config
	if err := json.Unmarshal([]byte(`{"source": {"type": "carrier-pigeon"}}`), &cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := news.New(&cfg); err == nil {
		t.Fatal("an unknown source must fail loudly on start, not silently serve nothing")
	}
}
