package health

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/laminara/laminara/server/internal/humanize"
)

const (
	probeTimeout = 2 * time.Second
	cacheFor     = 5 * time.Second
)

type Check struct {
	Name  string
	Probe func(ctx context.Context) error
}

type Handler struct {
	started time.Time
	checks  []Check
	log     *slog.Logger

	mu       sync.Mutex
	answered time.Time
	failures map[string]string
}

func New(checks ...Check) *Handler {
	return &Handler{started: time.Now(), checks: checks, log: slog.Default()}
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", h.live)
	mux.HandleFunc("/readyz", h.ready)
}

func (h *Handler) live(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, map[string]any{
		"status": "ok",
		"uptime": humanize.Duration(time.Since(h.started)),
	})
}

func (h *Handler) ready(w http.ResponseWriter, r *http.Request) {
	failures := h.run(r.Context())
	if len(failures) == 0 {
		write(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}

	names := make([]string, 0, len(failures))
	for name := range failures {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		h.log.Warn("проверка готовности не прошла", "source", "health", "проверка", name, "причина", failures[name])
	}

	write(w, http.StatusServiceUnavailable, map[string]any{
		"status": "degraded",
		"failed": names,
	})
}

func (h *Handler) run(ctx context.Context) map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()

	if time.Since(h.answered) < cacheFor && h.failures != nil {
		return h.failures
	}

	failures := map[string]string{}
	for _, check := range h.checks {
		probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
		err := check.Probe(probeCtx)
		cancel()
		if err != nil {
			failures[check.Name] = err.Error()
		}
	}

	h.answered = time.Now()
	h.failures = failures
	return failures
}

func write(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
