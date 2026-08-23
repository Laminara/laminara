package providers_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/laminara/laminara/server/internal/auth"
)

func TestHTTPProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["username"] == "neo" && body["password"] == "matrix" {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "uuid": "00000000-0000-0000-0000-000000000009"})
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	config, _ := json.Marshal(map[string]any{"url": server.URL, "successField": "ok", "uuidField": "uuid"})
	provider, err := auth.BuildProvider("http", config)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "neo", Password: "matrix"})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if identity.Username != "neo" {
		t.Fatalf("got %+v", identity)
	}
	if _, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "neo", Password: "wrong"}); err == nil {
		t.Fatal("wrong password accepted")
	}
}
