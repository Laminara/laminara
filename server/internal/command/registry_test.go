package command

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
)

func TestDispatchRunsCommand(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "echo", Run: func(_ context.Context, args []string, out io.Writer) error {
		fmt.Fprint(out, strings.Join(args, " "))
		return nil
	}})
	var buf bytes.Buffer
	if err := r.Dispatch(context.Background(), "echo hello world", &buf); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hello world" {
		t.Fatalf("got %q", buf.String())
	}
}

func TestDispatchUnknown(t *testing.T) {
	r := NewRegistry()
	if err := r.Dispatch(context.Background(), "nope", io.Discard); err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestDispatchEmptyIsNoOp(t *testing.T) {
	r := NewRegistry()
	if err := r.Dispatch(context.Background(), "   ", io.Discard); err != nil {
		t.Fatalf("empty line should be a no-op, got %v", err)
	}
}

func TestListIsSorted(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "b"})
	r.Register(Command{Name: "a"})
	list := r.List()
	if list[0].Name != "a" || list[1].Name != "b" {
		t.Fatalf("not sorted: %v", list)
	}
}

func TestLookupFindsCommandsAndAliases(t *testing.T) {
	r := NewRegistry()
	r.Register(Command{Name: "auth", Aliases: []string{"a"}, Secret: true})

	if got, ok := r.Lookup("auth"); !ok || !got.Secret {
		t.Fatalf("Lookup(\"auth\") = %+v, %v", got, ok)
	}
	if got, ok := r.Lookup("a"); !ok || !got.Secret {
		t.Fatalf("an alias must carry the same command, got %+v, %v", got, ok)
	}
	if _, ok := r.Lookup("nope"); ok {
		t.Fatal("an unknown name must not resolve")
	}
}
