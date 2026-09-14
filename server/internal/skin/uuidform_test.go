package skin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUUIDKeepsItsDashesAndHashDropsThem(t *testing.T) {
	raw, _ := json.Marshal(map[string]string{
		"skin": "https://cdn.example.com/by-uuid/%uuid%.png",
		"cape": "https://cdn.example.com/by-hash/%hash%.png",
	})
	provider, err := newTemplate(raw)
	if err != nil {
		t.Fatal(err)
	}
	textures, err := provider.Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn.example.com/by-uuid/8667ba71-b85a-4004-af54-457a9734eed7.png" {
		t.Fatalf("%%uuid%% обязан оставаться с дефисами, как обещано в документации: %s", textures.SkinURL)
	}
	if textures.CapeURL != "https://cdn.example.com/by-hash/8667ba71b85a4004af54457a9734eed7.png" {
		t.Fatalf("%%hash%% обязан быть без дефисов: %s", textures.CapeURL)
	}
}

func TestTheJSONSourceIsAskedWithDashes(t *testing.T) {
	var asked string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = r.URL.Path
		w.Write([]byte(`{"skin": "https://cdn.example.com/Steve.png"}`))
	}))
	defer server.Close()

	raw, _ := json.Marshal(map[string]string{"url": server.URL + "/profile/%uuid%"})
	provider, err := newJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(asked, "8667ba71-b85a-4004-af54-457a9734eed7") {
		t.Fatalf("источник скинов спросили по %q — сайты обычно хранят uuid с дефисами и ответят 404", asked)
	}
}

func TestAPlayerWithoutASkinIsNotAnOutage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"status":"not_found"}`))
	}))
	defer server.Close()

	raw, _ := json.Marshal(map[string]string{"url": server.URL + "/profile/%uuid%"})
	provider, err := newJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	textures, err := provider.Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7")
	if err != nil {
		t.Fatalf("404 по игроку без скина — обычное дело, а не поломка источника: %v", err)
	}
	if textures.SkinURL != "" {
		t.Fatalf("скина быть не должно: %+v", textures)
	}
}

func TestARealOutageIsStillAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	raw, _ := json.Marshal(map[string]string{"url": server.URL + "/profile/%uuid%"})
	provider, _ := newJSON(raw)
	if _, err := provider.Textures(context.Background(), "Steve", "8667ba71-b85a-4004-af54-457a9734eed7"); err == nil {
		t.Fatal("пятисотка источника должна доходить до оператора, а не выглядеть как «скина нет»")
	}
}
