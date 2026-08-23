package logbus

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

type Line struct {
	Time    time.Time
	Level   slog.Level
	Source  string
	Message string
	Fields  map[string]string
}

type Bus struct {
	mu   sync.Mutex
	ring []Line
	size int
	next int
	full bool
	subs map[chan Line]struct{}
}

func NewBus(size int) *Bus {
	return &Bus{
		ring: make([]Line, size),
		size: size,
		subs: make(map[chan Line]struct{}),
	}
}

func (b *Bus) publish(l Line) {
	b.mu.Lock()
	b.ring[b.next] = l
	b.next = (b.next + 1) % b.size
	if b.next == 0 {
		b.full = true
	}
	for ch := range b.subs {
		select {
		case ch <- l:
		default:
		}
	}
	b.mu.Unlock()
}

func (b *Bus) snapshotLocked(n int) []Line {
	var all []Line
	if b.full {
		all = append(all, b.ring[b.next:]...)
	}
	all = append(all, b.ring[:b.next]...)
	if n >= 0 && n < len(all) {
		all = all[len(all)-n:]
	}
	return all
}

func (b *Bus) Subscribe(backscroll int) (history []Line, live chan Line, cancel func()) {
	live = make(chan Line, 256)
	b.mu.Lock()
	history = b.snapshotLocked(backscroll)
	b.subs[live] = struct{}{}
	b.mu.Unlock()
	cancel = func() {
		b.mu.Lock()
		if _, ok := b.subs[live]; ok {
			delete(b.subs, live)
			close(live)
		}
		b.mu.Unlock()
	}
	return history, live, cancel
}

type Handler struct {
	inner slog.Handler
	bus   *Bus
	attrs []slog.Attr
	group string
}

func NewHandler(w io.Writer, level slog.Leveler, bus *Bus) *Handler {
	return &Handler{
		inner: slog.NewTextHandler(w, &slog.HandlerOptions{Level: level}),
		bus:   bus,
	}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	fields := make(map[string]string, len(h.attrs)+r.NumAttrs())
	source := ""
	collect := func(a slog.Attr) {
		if a.Key == "source" && h.group == "" {
			source = a.Value.String()
			return
		}
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		fields[key] = a.Value.String()
	}
	for _, a := range h.attrs {
		collect(a)
	}
	r.Attrs(func(a slog.Attr) bool {
		collect(a)
		return true
	})
	h.bus.publish(Line{
		Time:    r.Time,
		Level:   r.Level,
		Source:  source,
		Message: r.Message,
		Fields:  fields,
	})
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{
		inner: h.inner.WithAttrs(attrs),
		bus:   h.bus,
		attrs: append(append([]slog.Attr{}, h.attrs...), attrs...),
		group: h.group,
	}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	group := name
	if h.group != "" {
		group = h.group + "." + name
	}
	return &Handler{
		inner: h.inner.WithGroup(name),
		bus:   h.bus,
		attrs: h.attrs,
		group: group,
	}
}
