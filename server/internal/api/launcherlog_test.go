package api_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"connectrpc.com/connect"

	apiv1 "github.com/laminara/laminara/gen/go/laminara/api/v1"
	"github.com/laminara/laminara/gen/go/laminara/api/v1/apiv1connect"
	"github.com/laminara/laminara/server/internal/api"
	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/clientaddr"
	"github.com/laminara/laminara/server/internal/crash"
	"github.com/laminara/laminara/server/internal/ratelimit"
)

type launcherLogServer struct {
	client apiv1connect.LauncherServiceClient
	auth   *auth.Service
	dir    string
}

func startLauncherLogServer(t *testing.T, extra map[string]any) launcherLogServer {
	t.Helper()
	dir := t.TempDir()
	settings := map[string]any{
		"enabled": true,
		"sinks":   map[string]any{"на диск": map[string]any{"type": "file", "config": map[string]string{"dir": dir}}},
	}
	for key, value := range extra {
		settings[key] = value
	}
	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	var cfg crash.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	crashes, err := crash.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	proxies, err := clientaddr.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	limits, err := ratelimit.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	accounts := auth.NewService(secondFactorProvider{}, auth.NewMemorySessionStore(), auth.DefaultConfig())
	service := api.NewService(api.Options{
		Auth:    accounts,
		Crashes: crashes,
		Proxies: proxies,
		Limits:  limits,
		Log:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	server := httptest.NewServer(api.Handler(service, nil, false))
	t.Cleanup(server.Close)
	return launcherLogServer{
		client: apiv1connect.NewLauncherServiceClient(http.DefaultClient, server.URL),
		auth:   accounts,
		dir:    dir,
	}
}

func (s launcherLogServer) delivered(t *testing.T) string {
	t.Helper()
	entries, err := os.ReadDir(s.dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("в папке доставки %d файлов: %v", len(entries), err)
	}
	body, err := os.ReadFile(filepath.Join(s.dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func launcherLogRequest() *connect.Request[apiv1.ReportLauncherLogRequest] {
	return connect.NewRequest(&apiv1.ReportLauncherLogRequest{Report: &apiv1.LauncherLogReport{
		Log:     "ERROR laminara_lib::commands: sync failed for Vanilla",
		Details: map[string]string{"launcher": "1.15.0"},
	}})
}

func TestALauncherLogArrivesWithoutSigningIn(t *testing.T) {
	server := startLauncherLogServer(t, nil)
	response, err := server.client.ReportLauncherLog(context.Background(), launcherLogRequest())
	if err != nil {
		t.Fatal(err)
	}
	if !response.Msg.Accepted {
		t.Fatalf("журнал без входа отклонён: %s", response.Msg.Message)
	}
	body := server.delivered(t)
	for _, want := range []string{"игрок без входа — журнал лаунчера", "Адрес: 127.0.0.1", "sync failed for Vanilla"} {
		if !strings.Contains(body, want) {
			t.Errorf("в доставленном журнале нет %q:\n%s", want, body)
		}
	}
}

func TestAnOperatorCanRequireSigningInForLauncherLogs(t *testing.T) {
	server := startLauncherLogServer(t, map[string]any{"anonymous": false})
	response, err := server.client.ReportLauncherLog(context.Background(), launcherLogRequest())
	if err != nil {
		t.Fatal(err)
	}
	if response.Msg.Accepted {
		t.Fatal("журнал без входа принят, хотя оператор это выключил")
	}
}

func TestASignedInPlayerSendsTheLogUnderTheirName(t *testing.T) {
	server := startLauncherLogServer(t, map[string]any{"anonymous": false})
	tokens, err := server.auth.Login(context.Background(), "neo", "matrix", "654321")
	if err != nil {
		t.Fatal(err)
	}
	request := launcherLogRequest()
	request.Header().Set("Authorization", "Bearer "+tokens.Access)
	response, err := server.client.ReportLauncherLog(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Msg.Accepted {
		t.Fatalf("журнал вошедшего игрока отклонён: %s", response.Msg.Message)
	}
	if body := server.delivered(t); !strings.Contains(body, "neo — журнал лаунчера") {
		t.Fatalf("журнал подписан не ником игрока:\n%s", body)
	}
}
