package controlauth

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/command"
)

func TestTokensPersistOnlyHashesAndRevokeImmediately(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tokens.json")
	store := NewStore(path)
	token, secret, err := store.Create("operator", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || strings.Contains(string(data), secret) {
		t.Fatal("raw token persisted")
	}
	if _, err := NewStore(path).Authenticate(secret); err != nil {
		t.Fatal(err)
	}
	if err := store.Revoke(token.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(secret); err == nil {
		t.Fatal("revoked token accepted")
	}
	if _, err := store.Authenticate(secret + "x"); err == nil {
		t.Fatal("altered token accepted")
	}
}

func TestReadTokenCannotWriteThroughRPCOrCommandAlias(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	_, secret, err := store.Create("reader", []string{"read"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	registry := command.NewRegistry()
	executed := false
	registry.Register(command.Command{Name: "delete", Aliases: []string{"remove"}, Run: func(context.Context, []string, io.Writer) error { executed = true; return nil }})
	handler := store.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := registry.DispatchArgs(r.Context(), []string{"remove", "pack"}, io.Discard); err == nil {
			t.Error("reader executed mutation")
		}
		w.WriteHeader(http.StatusOK)
	}))
	for _, test := range []struct {
		method, token string
		status        int
	}{{"Status", "", 401}, {"ApplySettings", secret, 403}, {"Exec", secret, 200}} {
		req := httptest.NewRequest(http.MethodPost, "/laminara.admin.v1.AdminService/"+test.method, nil)
		if test.token != "" {
			req.Header.Set("Authorization", "Bearer "+test.token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != test.status {
			t.Fatalf("%s: %d", test.method, w.Code)
		}
	}
	if executed {
		t.Fatal("mutation happened")
	}
}

func TestRevocationCancelsActiveStream(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "tokens.json"))
	token, secret, err := store.Create("logs", []string{"logs"}, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	started, stopped := make(chan struct{}), make(chan struct{})
	handler := store.Handler(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { close(started); <-r.Context().Done(); close(stopped) }))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/laminara.admin.v1.AdminService/StreamLogs", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+secret)
	done := make(chan struct{})
	go func() { defer close(done); handler.ServeHTTP(httptest.NewRecorder(), req) }()
	<-started
	if err := store.Revoke(token.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(4 * time.Second):
		t.Fatal("revocation did not cancel stream")
	}
	<-done
}
