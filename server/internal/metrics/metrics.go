package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

type Handler struct {
	started         time.Time
	requests        atomic.Uint64
	duration        atomic.Uint64
	errors          atomic.Uint64
	commands        atomic.Uint64
	commandErrors   atomic.Uint64
	commandDuration atomic.Uint64
}

func (h *Handler) ObserveCommand(elapsed time.Duration, err error) {
	h.commands.Add(1)
	h.commandDuration.Add(uint64(elapsed))
	if err != nil {
		h.commandErrors.Add(1)
	}
}

func New() *Handler { return &Handler{started: time.Now()} }

func (h *Handler) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			h.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		defer func() { h.requests.Add(1); h.duration.Add(uint64(time.Since(start))) }()
		writer := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(writer, r)
		if writer.status >= 400 {
			h.errors.Add(1)
		}
	})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	fmt.Fprintf(w, "laminara_uptime_seconds %g\nlaminara_go_goroutines %d\nlaminara_heap_bytes %d\nlaminara_http_requests_total %d\nlaminara_http_duration_seconds_total %g\n", time.Since(h.started).Seconds(), runtime.NumGoroutine(), memory.Alloc, h.requests.Load(), float64(h.duration.Load())/float64(time.Second))
	fmt.Fprintf(w, "laminara_http_errors_total %d\nlaminara_commands_total %d\nlaminara_command_errors_total %d\nlaminara_command_duration_seconds_total %g\n", h.errors.Load(), h.commands.Load(), h.commandErrors.Load(), float64(h.commandDuration.Load())/float64(time.Second))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	if code >= 200 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
