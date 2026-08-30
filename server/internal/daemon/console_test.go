package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/command"
)

func quietDaemon() *Daemon {
	daemon := New(Options{})
	daemon.log = slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	daemon.console = &bytes.Buffer{}
	return daemon
}

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

func TestSecretCommandAnswersTheOperatorNotTheJournal(t *testing.T) {
	daemon := quietDaemon()
	journal := &bytes.Buffer{}
	daemon.log = slog.New(slog.NewTextHandler(journal, nil))
	console := &bytes.Buffer{}
	daemon.console = console
	daemon.registry.Register(command.Command{
		Name:   "auth",
		Secret: true,
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			fmt.Fprint(out, "секрет: TOPSECRET\nстрока: otpauth://TOPSECRET")
			return nil
		},
	})

	daemon.dispatchLines(context.Background(), strings.NewReader("auth totp ivan\n"))

	if !strings.Contains(console.String(), "TOPSECRET") {
		t.Fatalf("the answer must reach the console: %q", console.String())
	}
	if strings.Contains(journal.String(), "TOPSECRET") {
		t.Fatalf("the answer must stay out of the journal: %s", journal.String())
	}
	if !strings.Contains(journal.String(), "command=auth") || !strings.Contains(journal.String(), "command succeeded") {
		t.Fatalf("the journal must keep the fact of the run: %s", journal.String())
	}
}

func TestPlainCommandStillAnswersThroughTheJournal(t *testing.T) {
	daemon := quietDaemon()
	journal := &bytes.Buffer{}
	daemon.log = slog.New(slog.NewTextHandler(journal, nil))
	daemon.registry.Register(command.Command{
		Name: "status",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			fmt.Fprint(out, "версия 1.2.3")
			return nil
		},
	})

	daemon.dispatchLines(context.Background(), strings.NewReader("status\n"))

	if !strings.Contains(journal.String(), "версия 1.2.3") {
		t.Fatalf("a plain answer belongs in the journal: %s", journal.String())
	}
	if console, ok := daemon.console.(*bytes.Buffer); ok && console.Len() != 0 {
		t.Fatalf("a plain answer must not bypass the journal: %q", console.String())
	}
}

func TestSecretCommandFailureKeepsTheTypedLineOutOfTheJournal(t *testing.T) {
	daemon := quietDaemon()
	journal := &bytes.Buffer{}
	daemon.log = slog.New(slog.NewTextHandler(journal, nil))
	daemon.registry.Register(command.Command{
		Name:   "auth",
		Secret: true,
		Run: func(_ context.Context, _ []string, _ io.Writer) error {
			return errors.New("пароль не подошёл")
		},
	})

	daemon.dispatchLines(context.Background(), strings.NewReader("auth test ivan password123\n"))

	if strings.Contains(journal.String(), "password123") {
		t.Fatalf("the typed line must stay out of the journal: %s", journal.String())
	}
	if !strings.Contains(journal.String(), "command=auth") || !strings.Contains(journal.String(), "пароль не подошёл") {
		t.Fatalf("the journal must name the command and the reason: %s", journal.String())
	}
}
