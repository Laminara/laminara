package remote

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	sdkmodule "github.com/laminara/laminara/sdk/go/module"
	"github.com/laminara/laminara/server/internal/events"
)

type fakeService struct {
	mu   sync.Mutex
	seen []string
}

func (f *fakeService) Configure(context.Context, []byte) error { return nil }

func (f *fakeService) Info(context.Context) (sdkmodule.Info, []sdkmodule.CommandSpec, []string, error) {
	return sdkmodule.Info{Name: "fake"}, nil, []string{"build.published"}, nil
}

func (f *fakeService) Execute(context.Context, string, []string, io.Writer) error { return nil }

func (f *fakeService) Emit(_ context.Context, topic string, data map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seen = append(f.seen, topic+":"+data["name"])
	return nil
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

func TestLoaderCloseIdempotent(t *testing.T) {
	loader := NewLoader(slog.New(slog.NewTextHandler(io.Discard, nil)))
	loader.Close()
	loader.Close()
}
