package skin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serve(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(body)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func provider(t *testing.T, url string) Provider {
	t.Helper()
	built, err := Build("json", json.RawMessage(`{"url":"`+url+`?u=%username%&h=%hash%"}`))
	if err != nil {
		t.Fatal(err)
	}
	return built
}

func TestJSONAcceptsFlatDocument(t *testing.T) {
	server := serve(t, `{"skin":"https://cdn.test/Steve.png","cape":"https://cdn.test/cape.png","model":"slim"}`)
	got, err := provider(t, server.URL).Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	if err != nil {
		t.Fatal(err)
	}
	if got.SkinURL != "https://cdn.test/Steve.png" || got.CapeURL != "https://cdn.test/cape.png" || !got.Slim {
		t.Fatalf("получили %+v", got)
	}
}

func TestJSONAcceptsGravitDocument(t *testing.T) {
	server := serve(t, `{"SKIN":{"url":"https://cdn.test/Steve.png","digest":"abc","metadata":{"model":"slim"}},"CAPE":{"url":"https://cdn.test/cape.png","digest":"def"}}`)
	got, err := provider(t, server.URL).Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	if err != nil {
		t.Fatal(err)
	}
	if got.SkinURL != "https://cdn.test/Steve.png" {
		t.Fatalf("скин: %+v", got)
	}
	if got.CapeURL != "https://cdn.test/cape.png" {
		t.Fatalf("плащ: %+v", got)
	}
	if !got.Slim {
		t.Fatal("модель slim не распознана")
	}
}

func TestJSONWithoutCape(t *testing.T) {
	server := serve(t, `{"SKIN":{"url":"https://cdn.test/Steve.png"}}`)
	got, err := provider(t, server.URL).Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	if err != nil {
		t.Fatal(err)
	}
	if got.CapeURL != "" || got.Slim {
		t.Fatalf("получили %+v", got)
	}
}

func TestSubstituteHash(t *testing.T) {
	got := substitute("https://cdn.test/%hash%.png", "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	want := "https://cdn.test/8667ba71b85a4004af54457a9734eed7.png"
	if got != want {
		t.Fatalf("получили %q, ждали %q", got, want)
	}
}

func TestJSONSendsHeadersAndCaches(t *testing.T) {
	var seen []string
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		seen = append(seen, r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(`{"SKIN":{"url":"https://cdn.test/Steve.png"}}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	built, err := Build("json", json.RawMessage(`{"url":"`+server.URL+`?u=%username%","headers":{"Authorization":"Bearer секрет"},"cacheTTL":"1m"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := built.Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7"); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) == 0 || seen[0] != "Bearer секрет" {
		t.Fatalf("заголовок не ушёл на сервер: %v", seen)
	}
	if hits != 1 {
		t.Fatalf("кэш не сработал: запросов %d вместо одного", hits)
	}
}

func TestJSONWithoutCacheAsksEveryTime(t *testing.T) {
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if _, err := w.Write([]byte(`{"skin":"https://cdn.test/Steve.png"}`)); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)

	built, err := Build("json", json.RawMessage(`{"url":"`+server.URL+`","cacheTTL":"0s"}`))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := built.Textures(context.Background(), "Steve", "id"); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 2 {
		t.Fatalf("без кэша ожидали 2 запроса, получили %d", hits)
	}
}
