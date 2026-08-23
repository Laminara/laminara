package builtin

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/module"
)

func TestModulesCommand(t *testing.T) {
	registry := module.NewRegistry()
	registry.Add(NewDiagnostics(registry))

	commands := command.NewRegistry()
	if err := registry.Load(module.CommandHost{Registry: commands}); err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(registry.Loaded()) != 1 {
		t.Fatalf("loaded modules = %d, want 1", len(registry.Loaded()))
	}

	var out bytes.Buffer
	if err := commands.Dispatch(context.Background(), "modules", &out); err != nil {
		t.Fatalf("modules command: %v", err)
	}
	if !strings.Contains(out.String(), "diagnostics") {
		t.Fatalf("modules output missing the module: %q", out.String())
	}

	out.Reset()
	if err := commands.Dispatch(context.Background(), "mods", &out); err != nil {
		t.Fatalf("alias mods: %v", err)
	}
	if !strings.Contains(out.String(), "diagnostics") {
		t.Fatalf("alias output missing the module: %q", out.String())
	}
}
