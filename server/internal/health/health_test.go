package health_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/health"
)

func ask(t *testing.T, handler *health.Handler, path string) (int, map[string]any) {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))

	var body map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("%s answered with something that is not JSON: %q", path, recorder.Body.String())
	}
	return recorder.Code, body
}

func TestLivenessAnswersWhileTheProcessRuns(t *testing.T) {
	handler := health.New(health.Check{
		Name:  "storage",
		Probe: func(context.Context) error { return errors.New("хранилище недоступно") },
	})

	code, body := ask(t, handler, "/healthz")
	if code != http.StatusOK {
		t.Fatalf("code = %d, want 200: liveness must not depend on a broken dependency", code)
	}
	if body["status"] != "ok" || body["uptime"] == "" {
		t.Fatalf("body = %v", body)
	}
}

func TestReadinessFailsWhenADependencyIsDown(t *testing.T) {
	handler := health.New(health.Check{
		Name:  "storage",
		Probe: func(context.Context) error { return errors.New("хранилище недоступно") },
	})

	code, body := ask(t, handler, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", code)
	}
	failed, _ := body["failed"].([]any)
	if len(failed) != 1 || failed[0] != "storage" {
		t.Fatalf("failed = %v, want [storage]", body["failed"])
	}
	if _, leaks := body["checks"]; leaks {
		t.Fatalf("наружу не отдаём причину сбоя с адресами — это карта топологии: %v", body["checks"])
	}
}

func TestReadinessPassesWhenEveryCheckPasses(t *testing.T) {
	handler := health.New(
		health.Check{Name: "storage", Probe: func(context.Context) error { return nil }},
		health.Check{Name: "catalog", Probe: func(context.Context) error { return nil }},
	)

	code, body := ask(t, handler, "/readyz")
	if code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("code = %d, body = %v", code, body)
	}
}

func TestSlowProbeDoesNotHangTheAnswer(t *testing.T) {
	handler := health.New(health.Check{
		Name: "storage",
		Probe: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		},
	})

	started := time.Now()
	code, _ := ask(t, handler, "/readyz")
	if code != http.StatusServiceUnavailable {
		t.Fatalf("code = %d, want 503", code)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("readiness waited %s for a stuck probe", elapsed)
	}
}

func TestAnswersAreCachedSoProbingIsCheap(t *testing.T) {
	calls := 0
	handler := health.New(health.Check{
		Name: "storage",
		Probe: func(context.Context) error {
			calls++
			return nil
		},
	})

	for i := 0; i < 5; i++ {
		if code, _ := ask(t, handler, "/readyz"); code != http.StatusOK {
			t.Fatalf("code = %d, want 200", code)
		}
	}
	if calls != 1 {
		t.Fatalf("probe ran %d times, want 1: a monitor polling every second must not hammer the storage", calls)
	}
}
