package daemon

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/command"
)

func commandFor(name string, run func(args []string)) command.Command {
	return command.Command{
		Name: name,
		Run: func(_ context.Context, args []string, _ io.Writer) error {
			run(args)
			return nil
		},
	}
}

func TestTypedLinesReachTheCommandRegistry(t *testing.T) {
	daemon := New(Options{})
	seen := make(chan string, 4)
	daemon.registry.Register(commandFor("echo", func(args []string) { seen <- strings.Join(args, " ") }))

	daemon.dispatchLines(context.Background(), strings.NewReader("echo hello\n\n   \necho world\n"))

	for _, want := range []string{"hello", "world"} {
		select {
		case got := <-seen:
			if got != want {
				t.Fatalf("got %q, want %q", got, want)
			}
		default:
			t.Fatalf("command %q never ran; blank lines must be skipped, not swallow the next one", want)
		}
	}
}

func TestUnknownCommandDoesNotStopTheConsole(t *testing.T) {
	daemon := New(Options{})
	ran := make(chan struct{}, 1)
	daemon.registry.Register(commandFor("ok", func([]string) { ran <- struct{}{} }))

	daemon.dispatchLines(context.Background(), strings.NewReader("nosuchcommand\nok\n"))

	select {
	case <-ran:
	default:
		t.Fatal("a typo must not end the console for the rest of the session")
	}
}

func TestCancelledContextStopsReading(t *testing.T) {
	daemon := New(Options{})
	ran := make(chan struct{}, 1)
	daemon.registry.Register(commandFor("ok", func([]string) { ran <- struct{}{} }))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	daemon.dispatchLines(ctx, strings.NewReader("ok\n"))

	select {
	case <-ran:
		t.Fatal("commands must not run once the daemon is shutting down")
	default:
	}
}
