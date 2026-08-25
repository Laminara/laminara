package crash

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	MaxLogBytes  = 256 << 10
	maxLogInText = 3800
)

type Report struct {
	Player    string
	UUID      string
	Build     string
	Version   string
	Loader    string
	ExitCode  int32
	Log       string
	Details   map[string]string
	Happened  time.Time
	Launcher  string
	Platform  string
	OSVersion string
}

type Sink interface {
	Name() string
	Send(ctx context.Context, report Report) error
}

type SinkFactory func(config json.RawMessage) (Sink, error)

var factories = map[string]SinkFactory{}

func RegisterSink(name string, factory SinkFactory) {
	factories[name] = factory
}

func SinkNames() []string {
	names := make([]string, 0, len(factories))
	for name := range factories {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

type SinkConfig struct {
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type Config struct {
	Enabled    bool                  `json:"enabled"`
	MaxPerHour int                   `json:"maxPerHour"`
	Sinks      map[string]SinkConfig `json:"sinks"`
}

func (r Report) Title() string {
	who := r.Player
	if who == "" {
		who = "неизвестный игрок"
	}
	build := r.Build
	if build == "" {
		build = "без сборки"
	}
	return fmt.Sprintf("%s — падение в сборке «%s»", who, build)
}

func (r Report) Lines() []string {
	lines := []string{
		"Игрок: " + fallback(r.Player, "неизвестен"),
		"Сборка: " + fallback(r.Build, "не указана"),
	}
	if r.Version != "" {
		lines = append(lines, "Версия сборки: "+r.Version)
	}
	if r.Loader != "" {
		lines = append(lines, "Загрузчик: "+r.Loader)
	}
	lines = append(lines, fmt.Sprintf("Код выхода: %d", r.ExitCode))
	if r.Platform != "" {
		lines = append(lines, "Система: "+strings.TrimSpace(r.Platform+" "+r.OSVersion))
	}
	if r.Launcher != "" {
		lines = append(lines, "Лаунчер: "+r.Launcher)
	}
	if !r.Happened.IsZero() {
		lines = append(lines, "Когда: "+r.Happened.Local().Format("02.01.2006 15:04:05"))
	}

	keys := make([]string, 0, len(r.Details))
	for key := range r.Details {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		lines = append(lines, key+": "+r.Details[key])
	}
	return lines
}

func (r Report) Text() string {
	return strings.Join(r.Lines(), "\n")
}

func (r Report) LogTail(limit int) string {
	log := strings.TrimSpace(r.Log)
	if limit <= 0 || len(log) <= limit {
		return log
	}
	trimmed := log[len(log)-limit:]
	if cut := strings.IndexByte(trimmed, '\n'); cut >= 0 && cut+1 < len(trimmed) {
		trimmed = trimmed[cut+1:]
	}
	return "…\n" + trimmed
}

func fallback(value, instead string) string {
	if strings.TrimSpace(value) == "" {
		return instead
	}
	return value
}
