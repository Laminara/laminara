package builtin

import (
	"context"
	"fmt"
	"io"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/module"
)

type Diagnostics struct {
	registry *module.Registry
}

func NewDiagnostics(registry *module.Registry) *Diagnostics {
	return &Diagnostics{registry: registry}
}

func (d *Diagnostics) Info() module.Info {
	return module.Info{Name: "diagnostics", Version: "1.0.0", Description: "Список загруженных модулей"}
}

func (d *Diagnostics) Register(host module.Host) error {
	host.AddCommand(command.Command{
		Name:     "modules",
		Aliases:  []string{"mods"},
		Synopsis: "показать загруженные модули",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			loaded := d.registry.Loaded()
			if len(loaded) == 0 {
				fmt.Fprintln(out, "модули не загружены")
				return nil
			}
			for _, info := range loaded {
				fmt.Fprintf(out, "%-16s %-8s %s\n", info.Name, info.Version, info.Description)
			}
			return nil
		},
	})
	return nil
}
