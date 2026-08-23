package access_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/laminara/laminara/server/internal/access"
)

func controllerFor(t *testing.T, raw string) *access.Controller {
	t.Helper()
	var cfg access.Config
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		t.Fatalf("config: %v", err)
	}
	controller, err := access.New(&cfg)
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	return controller
}

func writeFile(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestNilControllerAllowsEverything(t *testing.T) {
	var controller *access.Controller
	if !controller.Decide(context.Background(), "anything", access.Subject{}).Allowed {
		t.Fatal("a server with no access config must serve every build")
	}
	if controller.Guarded() {
		t.Fatal("objects must stay public when no rule exists")
	}
}

func TestUnclaimedBuildStaysPublic(t *testing.T) {
	path := writeFile(t, "list.json", `["neo"]`)
	controller := controllerFor(t, fmt.Sprintf(`{
		"sources": {"staff": {"type": "file", "config": {"path": %q}}},
		"rules": [{"builds": ["test-*"], "source": "staff"}]
	}`, path))

	if !controller.Decide(context.Background(), "survival", access.Subject{}).Allowed {
		t.Fatal("a build no rule matches must remain public")
	}
	if !controller.Guarded() {
		t.Fatal("objects must be guarded once a rule exists")
	}
}

func TestFileSourceMatchesAndReloads(t *testing.T) {
	path := writeFile(t, "list.json", `{"users": ["Neo"], "uuids": ["11112222-3333-4444-5555-666677778888"]}`)
	controller := controllerFor(t, fmt.Sprintf(`{
		"sources": {"staff": {"type": "file", "config": {"path": %q}}},
		"rules": [{"builds": ["test-*"], "source": "staff", "message": "закрытый тест"}]
	}`, path))
	ctx := context.Background()

	if !controller.Decide(ctx, "test-alpha", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("username match is case-insensitive")
	}
	if !controller.Decide(ctx, "test-alpha", access.Subject{UUID: "11112222-3333-4444-5555-666677778888"}).Allowed {
		t.Fatal("uuid must match as written")
	}
	if !controller.Decide(ctx, "test-alpha", access.Subject{UUID: "11112222333344445555666677778888"}).Allowed {
		t.Fatal("uuid must match without dashes")
	}
	denied := controller.Decide(ctx, "test-alpha", access.Subject{Username: "smith"})
	if denied.Allowed || denied.Reason != "закрытый тест" {
		t.Fatalf("outsider must be denied with the configured message, got %+v", denied)
	}
	if controller.Decide(ctx, "test-alpha", access.Subject{}).Allowed {
		t.Fatal("anonymous callers must never pass a whitelist")
	}

	if err := os.WriteFile(path, []byte(`["smith"]`), 0o600); err != nil {
		t.Fatal(err)
	}
	controller.Reload()
	if !controller.Decide(ctx, "test-alpha", access.Subject{Username: "smith"}).Allowed {
		t.Fatal("editing the file must take effect")
	}
	if controller.Decide(ctx, "test-alpha", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("removing a player must take effect")
	}
}

func TestPerBuildRoster(t *testing.T) {
	path := writeFile(t, "list.json", `{"builds": {"test-alpha": ["neo"], "test-beta": ["smith"]}}`)
	controller := controllerFor(t, fmt.Sprintf(`{
		"sources": {"staff": {"type": "file", "config": {"path": %q}}},
		"rules": [{"builds": ["test-*"], "source": "staff"}]
	}`, path))
	ctx := context.Background()

	if !controller.Decide(ctx, "test-alpha", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("neo belongs to test-alpha")
	}
	if controller.Decide(ctx, "test-beta", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("one document must gate each build separately")
	}
}

func TestHiddenVisibility(t *testing.T) {
	path := writeFile(t, "list.txt", "neo\n# comment\n")
	controller := controllerFor(t, fmt.Sprintf(`{
		"sources": {"staff": {"type": "file", "config": {"path": %q}}},
		"rules": [{"builds": ["secret"], "source": "staff", "visibility": "hidden"}]
	}`, path))

	decision := controller.Decide(context.Background(), "secret", access.Subject{Username: "smith"})
	if decision.Allowed || !decision.Hidden {
		t.Fatalf("a hidden build must be denied and hidden, got %+v", decision)
	}
	if !controller.Decide(context.Background(), "secret", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("plain-text lists must work")
	}
}

func TestMissingFileFailsClosed(t *testing.T) {
	controller := controllerFor(t, `{
		"sources": {"staff": {"type": "file", "config": {"path": "/nonexistent/laminara-whitelist.json"}}},
		"rules": [{"builds": ["test"], "source": "staff"}]
	}`)
	if controller.Decide(context.Background(), "test", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("an unreadable whitelist must deny rather than open the build")
	}
}

func TestHTTPListModeCachesOneFetch(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("build") != "test" {
			t.Errorf("build not forwarded: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"users": ["neo"]}`)
	}))
	defer server.Close()

	controller := controllerFor(t, fmt.Sprintf(`{
		"sources": {"site": {"type": "http", "config": {"url": %q, "cacheTTL": "1h"}}},
		"rules": [{"builds": ["test"], "source": "site"}]
	}`, server.URL))
	ctx := context.Background()

	for range 5 {
		if !controller.Decide(ctx, "test", access.Subject{Username: "neo"}).Allowed {
			t.Fatal("neo is on the list")
		}
	}
	if controller.Decide(ctx, "test", access.Subject{Username: "smith"}).Allowed {
		t.Fatal("smith is not on the list")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("the list must be fetched once per TTL, got %d fetches", got)
	}
}

func TestHTTPCheckMode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("username") == "neo" {
			fmt.Fprint(w, `{"allowed": true}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	controller := controllerFor(t, fmt.Sprintf(`{
		"sources": {"site": {"type": "http", "config": {"url": %q, "mode": "check"}}},
		"rules": [{"builds": ["test"], "source": "site"}]
	}`, server.URL))
	ctx := context.Background()

	if !controller.Decide(ctx, "test", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("the endpoint approved neo")
	}
	if controller.Decide(ctx, "test", access.Subject{Username: "smith"}).Allowed {
		t.Fatal("403 must deny")
	}
}

func TestHTTPOutageFailsClosedUnlessAsked(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	closed := controllerFor(t, fmt.Sprintf(`{
		"sources": {"site": {"type": "http", "config": {"url": %q}}},
		"rules": [{"builds": ["test"], "source": "site"}]
	}`, server.URL))
	if closed.Decide(context.Background(), "test", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("a broken endpoint must not hand out access")
	}

	open := controllerFor(t, fmt.Sprintf(`{
		"sources": {"site": {"type": "http", "config": {"url": %q, "failOpen": true}}},
		"rules": [{"builds": ["test"], "source": "site"}]
	}`, server.URL))
	if !open.Decide(context.Background(), "test", access.Subject{Username: "neo"}).Allowed {
		t.Fatal("failOpen must keep players in when the endpoint is down")
	}
}

func TestPublicObjectsOptOut(t *testing.T) {
	path := writeFile(t, "list.json", `["neo"]`)
	controller := controllerFor(t, fmt.Sprintf(`{
		"publicObjects": true,
		"sources": {"staff": {"type": "file", "config": {"path": %q}}},
		"rules": [{"builds": ["test"], "source": "staff"}]
	}`, path))
	if controller.Guarded() {
		t.Fatal("publicObjects must turn the object guard off")
	}
}

func TestConfigErrorsAreLoud(t *testing.T) {
	cases := map[string]string{
		"unknown source type": `{"sources": {"x": {"type": "carrier-pigeon"}}, "rules": []}`,
		"dangling source":     `{"rules": [{"builds": ["a"], "source": "missing"}]}`,
		"no builds":           `{"sources": {"s": {"type": "file", "config": {"path": "/tmp/x"}}}, "rules": [{"source": "s"}]}`,
		"bad visibility":      `{"sources": {"s": {"type": "file", "config": {"path": "/tmp/x"}}}, "rules": [{"builds": ["a"], "source": "s", "visibility": "invisible"}]}`,
	}
	for name, raw := range cases {
		var cfg access.Config
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := access.New(&cfg); err == nil {
			t.Fatalf("%s: expected a startup error", name)
		}
	}
}
