package events

import "sync"

type Event struct {
	Topic string
	Data  map[string]string
}

type Bus struct {
	mu   sync.RWMutex
	subs []func(Event)
}

func NewBus() *Bus {
	return &Bus{}
}

func (b *Bus) Subscribe(fn func(Event)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.subs = append(b.subs, fn)
}

func (b *Bus) Publish(e Event) {
	b.mu.RLock()
	subs := make([]func(Event), len(b.subs))
	copy(subs, b.subs)
	b.mu.RUnlock()
	for _, fn := range subs {
		fn(e)
	}
}
