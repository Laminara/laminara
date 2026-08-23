package events

import "testing"

func TestBusPublishReachesSubscribers(t *testing.T) {
	bus := NewBus()
	var seen []Event
	bus.Subscribe(func(e Event) { seen = append(seen, e) })
	bus.Subscribe(func(e Event) { seen = append(seen, e) })

	bus.Publish(Event{Topic: "build.published", Data: map[string]string{"name": "Survival"}})

	if len(seen) != 2 {
		t.Fatalf("both subscribers must fire, got %d", len(seen))
	}
	if seen[0].Topic != "build.published" || seen[0].Data["name"] != "Survival" {
		t.Fatalf("event not delivered intact: %+v", seen[0])
	}
}

func TestBusNoSubscribers(t *testing.T) {
	NewBus().Publish(Event{Topic: "x"})
}
