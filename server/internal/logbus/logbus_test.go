package logbus

import (
	"log/slog"
	"testing"
	"time"
)

func line(msg string) Line {
	return Line{Time: time.Unix(0, 0), Level: slog.LevelInfo, Message: msg}
}

func messages(lines []Line) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = l.Message
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRingWrapsToNewest(t *testing.T) {
	b := NewBus(3)
	for _, m := range []string{"a", "b", "c", "d"} {
		b.publish(line(m))
	}
	history, _, cancel := b.Subscribe(-1)
	defer cancel()
	if got, want := messages(history), []string{"b", "c", "d"}; !equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestBackscrollLimit(t *testing.T) {
	b := NewBus(10)
	for _, m := range []string{"a", "b", "c", "d"} {
		b.publish(line(m))
	}
	history, _, cancel := b.Subscribe(2)
	defer cancel()
	if got, want := messages(history), []string{"c", "d"}; !equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestZeroBackscrollHasNoHistory(t *testing.T) {
	b := NewBus(4)
	b.publish(line("a"))
	history, _, cancel := b.Subscribe(0)
	defer cancel()
	if len(history) != 0 {
		t.Fatalf("expected no history, got %v", messages(history))
	}
}

func TestLiveDelivery(t *testing.T) {
	b := NewBus(4)
	_, live, cancel := b.Subscribe(0)
	defer cancel()
	b.publish(line("x"))
	select {
	case l := <-live:
		if l.Message != "x" {
			t.Fatalf("got %q", l.Message)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for live line")
	}
}

func TestCancelUnsubscribes(t *testing.T) {
	b := NewBus(4)
	_, _, cancel := b.Subscribe(0)
	cancel()
	b.publish(line("y"))
	b.mu.Lock()
	n := len(b.subs)
	b.mu.Unlock()
	if n != 0 {
		t.Fatalf("subscriber not removed, %d left", n)
	}
}
