package metrics_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/metrics"
)

func TestMetricsCountFailuresWithoutRequestOrCommandValues(t *testing.T) {
	meter := metrics.New()
	registry := command.NewRegistry()
	registry.SetObserver(meter.ObserveCommand)
	registry.Register(command.Command{Name: "private-name", Run: func(context.Context, []string, io.Writer) error { return errors.New("private-error") }})
	_ = registry.DispatchArgs(t.Context(), []string{"private-name", "private-token"}, io.Discard)
	handler := meter.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/private-path", nil))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := response.Body.String()
	for _, want := range []string{"laminara_http_requests_total 1\n", "laminara_http_errors_total 1\n", "laminara_commands_total 1\n", "laminara_command_errors_total 1\n"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "private-") {
		t.Fatal("private data in metrics")
	}
}

func TestMetricsPreserveWebSocketUpgrade(t *testing.T) {
	result := make(chan error, 1)
	server := httptest.NewServer(metrics.New().Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		result <- err
		if err == nil {
			defer connection.CloseNow()
			_ = connection.Write(r.Context(), websocket.MessageText, []byte("ready"))
		}
	})))
	defer server.Close()
	connection, _, err := websocket.Dial(t.Context(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	_, data, err := connection.Read(t.Context())
	if err != nil || string(data) != "ready" {
		t.Fatalf("websocket response: %s %v", data, err)
	}
}
