package remote

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/auth"
	_ "github.com/laminara/laminara/server/internal/auth/providers"
	"github.com/laminara/laminara/server/internal/events"
	"github.com/laminara/laminara/server/internal/news"
	"github.com/laminara/laminara/server/internal/skin"
)

type fakeService struct {
	mu         sync.Mutex
	seen       []string
	providers  []sdkmodule.ProviderSpec
	openedWith string
	identity   sdkmodule.Identity
	authErr    error
}

func (f *fakeService) Configure(context.Context, []byte) error { return nil }

func (f *fakeService) Info(context.Context) (sdkmodule.Manifest, error) {
	return sdkmodule.Manifest{
		Info:      sdkmodule.Info{Name: "fake"},
		Events:    []string{"build.published"},
		Providers: f.providers,
	}, nil
}

func (f *fakeService) Execute(context.Context, string, []string, io.Writer) error { return nil }

func (f *fakeService) Emit(_ context.Context, topic string, data map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, topic+":"+data["name"])
	return nil
}

func (f *fakeService) OpenProvider(_ context.Context, _ sdkmodule.ProviderKind, _ string, config []byte) (sdkmodule.Handle, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.openedWith = string(config)
	return 7, nil
}

func (f *fakeService) Authenticate(context.Context, sdkmodule.Handle, sdkmodule.Credentials) (sdkmodule.Identity, error) {
	return f.identity, f.authErr
}

func (f *fakeService) Textures(context.Context, sdkmodule.Handle, string, string) (sdkmodule.Textures, error) {
	return sdkmodule.Textures{SkinURL: "https://cms/skin.png", Slim: true}, nil
}

func (f *fakeService) Allows(context.Context, sdkmodule.Handle, string, sdkmodule.Subject) (bool, error) {
	return true, nil
}

func (f *fakeService) NewsItems(context.Context, sdkmodule.Handle) ([]sdkmodule.NewsItem, error) {
	return []sdkmodule.NewsItem{{ID: "1", Title: "Открытие", PublishedAt: time.Unix(1700000000, 0).UTC()}}, nil
}

func TestLoaderEventOrderingAndFilter(t *testing.T) {
	svc := &fakeService{}
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	loader.handlers = append(loader.handlers, &eventTarget{
		name:   "fake",
		topics: map[string]bool{"build.published": true},
		svc:    svc,
		queue:  make(chan events.Event, eventQueueSize),
	})
	bus := events.NewBus()
	loader.Subscribe(bus)
	defer loader.Close()

	for i := 0; i < 20; i++ {
		bus.Publish(events.Event{Topic: "build.published", Data: map[string]string{"name": strconv.Itoa(i)}})
	}
	bus.Publish(events.Event{Topic: "build.deleted", Data: map[string]string{"name": "ignored"}})

	for i := 0; i < 200; i++ {
		svc.mu.Lock()
		n := len(svc.seen)
		svc.mu.Unlock()
		if n == 20 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	svc.mu.Lock()
	defer svc.mu.Unlock()
	if len(svc.seen) != 20 {
		t.Fatalf("expected 20 delivered events (deleted topic filtered), got %d: %v", len(svc.seen), svc.seen)
	}
	for i := 0; i < 20; i++ {
		if svc.seen[i] != "build.published:"+strconv.Itoa(i) {
			t.Fatalf("events out of order at %d: %v", i, svc.seen)
		}
	}
}

func TestLoaderRegistersProviders(t *testing.T) {
	svc := &fakeService{
		providers: []sdkmodule.ProviderSpec{
			{Kind: sdkmodule.ProviderAuth, Name: "test-cms"},
			{Kind: sdkmodule.ProviderSkin, Name: "test-cms"},
			{Kind: sdkmodule.ProviderAccess, Name: "test-cms"},
			{Kind: sdkmodule.ProviderNews, Name: "test-cms"},
		},
		identity: sdkmodule.Identity{Subject: "42", Username: "neo", UUID: "d1b0f4a2-0000-4000-8000-000000000001"},
	}
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	loader.registerProviders("fake", svc.providers, svc)

	provider, err := auth.BuildProvider("test-cms", json.RawMessage(`{"url":"https://cms"}`))
	if err != nil {
		t.Fatal(err)
	}
	if svc.openedWith != `{"url":"https://cms"}` {
		t.Fatalf("provider config not delivered: %q", svc.openedWith)
	}
	identity, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "neo", Password: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Username != "neo" || identity.UUID.String() != "d1b0f4a2-0000-4000-8000-000000000001" {
		t.Fatalf("identity = %+v", identity)
	}

	textures, err := buildSkin(t, "test-cms")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cms/skin.png" || !textures.Slim {
		t.Fatalf("textures = %+v", textures)
	}

	announcements, err := news.New(&news.Config{Source: news.SourceConfig{Type: "test-cms"}})
	if err != nil {
		t.Fatal(err)
	}
	items := announcements.Latest(context.Background())
	if len(items) != 1 || items[0].PublishedAtUnixNanos != time.Unix(1700000000, 0).UnixNano() {
		t.Fatalf("news items = %+v", items)
	}

	if !slices.Contains(access.SourceNames(), "test-cms") {
		t.Fatalf("access sources = %v", access.SourceNames())
	}
	if err := registerProvider(sdkmodule.ProviderSpec{Kind: sdkmodule.ProviderAuth, Name: "test-cms"}, svc); err == nil {
		t.Fatal("a name already taken must be refused instead of silently replacing the provider")
	}
	if err := registerProvider(sdkmodule.ProviderSpec{Kind: sdkmodule.ProviderAuth, Name: "http"}, svc); err == nil {
		t.Fatal("a built-in provider name must be refused")
	}
}

func buildSkin(t *testing.T, name string) (skin.Textures, error) {
	t.Helper()
	provider, err := skin.Build(name, json.RawMessage(`{}`))
	if err != nil {
		return skin.Textures{}, err
	}
	return provider.Textures(context.Background(), "neo", "d1b0f4a2-0000-4000-8000-000000000001")
}

func TestLoaderCloseIdempotent(t *testing.T) {
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	loader.Close()
	loader.Close()
}
