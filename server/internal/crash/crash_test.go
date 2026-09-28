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

func launcherLog() crash.Report {
	return crash.Report{
		Kind:     crash.LauncherLog,
		Log:      "WARN laminara_core::sync: файл не скачался",
		Details:  map[string]string{"launcher": "1.15.0", "os": "Windows 11"},
		Happened: time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
	}
}

func onDisk(t *testing.T, dir string, extra map[string]any) *crash.Service {
	t.Helper()
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
	service, err := crash.New(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestALauncherLogIsNamedAsSuch(t *testing.T) {
	dir := t.TempDir()
	report := launcherLog()
	report.Player = "Dela1s"
	if err := onDisk(t, dir, nil).Accept(context.Background(), report, quiet()); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || !strings.HasSuffix(entries[0].Name(), "-launcher.log") {
		t.Fatalf("файл журнала лаунчера назван не так: %v", entries)
	}
	body, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.Contains(string(body), "Dela1s — журнал лаунчера") {
		t.Fatalf("заголовок не говорит, что это журнал лаунчера:\n%s", body)
	}
	if strings.Contains(string(body), "Код выхода") || strings.Contains(string(body), "падение") {
		t.Fatalf("у журнала лаунчера нет кода выхода и это не падение:\n%s", body)
	}
	if report.FileName() != "launcher.log" || sample().FileName() != "crash.log" {
		t.Fatal("вложение в Discord и Telegram должно называться по виду отчёта")
	}
}

func TestAnAnonymousLogIsLimitedByAddress(t *testing.T) {
	dir := t.TempDir()
	service := onDisk(t, dir, map[string]any{"anonymousPerHour": 2})
	for i := 0; i < 2; i++ {
		if err := service.AcceptAnonymous(context.Background(), launcherLog(), "203.0.113.7", quiet()); err != nil {
			t.Fatal(err)
		}
	}
	if err := service.AcceptAnonymous(context.Background(), launcherLog(), "203.0.113.7", quiet()); err == nil {
		t.Fatal("третий журнал без входа с того же адреса за час должен быть отклонён")
	}
	if err := service.AcceptAnonymous(context.Background(), launcherLog(), "198.51.100.4", quiet()); err != nil {
		t.Fatalf("соседний адрес не должен страдать от чужого лимита: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	body, _ := os.ReadFile(filepath.Join(dir, entries[0].Name()))
	if !strings.Contains(string(body), "без входа") || !strings.Contains(string(body), "Адрес: ") {
		t.Fatalf("анонимный журнал должен говорить, что игрок не вошёл, и откуда он:\n%s", body)
	}
}

func TestTheOperatorCanRefuseAnonymousLogs(t *testing.T) {
	service := onDisk(t, t.TempDir(), map[string]any{"anonymous": false})
	if err := service.AcceptAnonymous(context.Background(), launcherLog(), "203.0.113.7", quiet()); err == nil {
		t.Fatal("выключенные анонимные журналы не должны приниматься")
	}
}
