package crash_test

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
	"sync"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/crash"
)

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func sample() crash.Report {
	return crash.Report{
		Player:   "Игрок",
		Build:    "hitech",
		Version:  "3",
		Loader:   "forge",
		ExitCode: -1,
		Log:      "java.lang.NullPointerException\n\tat net.minecraft.client…",
		Details:  map[string]string{"os": "Windows 11", "launcher": "1.0.0"},
		Happened: time.Date(2026, 8, 25, 18, 30, 0, 0, time.UTC),
	}
}

func TestFileSinkWritesEverythingNeededToDebug(t *testing.T) {
	dir := t.TempDir()
	config, err := json.Marshal(map[string]any{
		"enabled": true,
		"sinks":   map[string]any{"на диск": map[string]any{"type": "file", "config": map[string]string{"dir": dir}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var cfg crash.Config
	if err := json.Unmarshal(config, &cfg); err != nil {
		t.Fatal(err)
	}

	service, err := crash.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Accept(context.Background(), sample(), quiet()); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("файлов в папке: %d", len(entries))
	}
	body, err := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Игрок", "hitech", "forge", "NullPointerException", "Windows 11"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("в отчёте нет %q", want)
		}
	}
}

func TestWebhookGetsTheReportAsJSON(t *testing.T) {
	var got map[string]any
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { _ = json.NewDecoder(r.Body).Decode(&got) })
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	cfg := crash.Config{
		Enabled: true,
		Sinks: map[string]crash.SinkConfig{
			"свой": {Type: "http", Config: json.RawMessage(`{"url":"` + server.URL + `"}`)},
		},
	}
	service, err := crash.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Accept(context.Background(), sample(), quiet()); err != nil {
		t.Fatal(err)
	}
	if got["player"] != "Игрок" || got["build"] != "hitech" {
		t.Fatalf("вебхук получил %v", got)
	}
	if !strings.Contains(got["log"].(string), "NullPointerException") {
		t.Fatal("журнал игры до вебхука не доехал")
	}
}

func TestOneBrokenAddressDoesNotLoseTheReport(t *testing.T) {
	dir := t.TempDir()
	cfg := crash.Config{
		Enabled: true,
		Sinks: map[string]crash.SinkConfig{
			"мимо":    {Type: "http", Config: json.RawMessage(`{"url":"http://127.0.0.1:1/nope"}`)},
			"на диск": {Type: "file", Config: json.RawMessage(`{"dir":"` + dir + `"}`)},
		},
	}
	service, err := crash.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := service.Accept(context.Background(), sample(), quiet()); err != nil {
		t.Fatalf("отчёт потерян из-за одного недоступного адреса: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Fatal("рабочий адрес отчёт не получил")
	}
}

func TestFloodIsCutOff(t *testing.T) {
	dir := t.TempDir()
	cfg := crash.Config{
		Enabled:    true,
		MaxPerHour: 2,
		Sinks:      map[string]crash.SinkConfig{"на диск": {Type: "file", Config: json.RawMessage(`{"dir":"` + dir + `"}`)}},
	}
	service, err := crash.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := service.Accept(context.Background(), sample(), quiet()); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.Accept(context.Background(), sample(), quiet()); err == nil {
		t.Fatal("третий отчёт за час от того же игрока должен быть отклонён")
	}
}

func TestSwitchedOffMeansNoService(t *testing.T) {
	service, err := crash.New(&crash.Config{Enabled: false})
	if err != nil {
		t.Fatal(err)
	}
	if service != nil {
		t.Fatal("выключённые отчёты не должны создавать службу")
	}
}

func TestEnabledWithoutAddressesIsRefused(t *testing.T) {
	if _, err := crash.New(&crash.Config{Enabled: true}); err == nil {
		t.Fatal("включить приём отчётов и никуда их не слать — это молчаливая потеря данных")
	}
}

func TestUnknownDeliveryIsRefused(t *testing.T) {
	cfg := crash.Config{
		Enabled: true,
		Sinks:   map[string]crash.SinkConfig{"куда-то": {Type: "почта"}},
	}
	if _, err := crash.New(&cfg); err == nil {
		t.Fatal("опечатка в способе доставки должна остановить запуск")
	}
}
