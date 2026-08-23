package module

import (
	"fmt"

	"github.com/laminara/laminara/server/internal/command"
)

type Info struct {
	Name        string
	Version     string
	Description string
}

type Host interface {
	AddCommand(command.Command)
}

type Module interface {
	Info() Info
	Register(Host) error
}

type Registry struct {
	modules []Module
	loaded  []Info
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Add(m Module) {
	r.modules = append(r.modules, m)
}

func (r *Registry) Load(host Host) error {
	for _, m := range r.modules {
		if err := m.Register(host); err != nil {
			return fmt.Errorf("module %q: %w", m.Info().Name, err)
		}
		r.loaded = append(r.loaded, m.Info())
	}
	return nil
}

func (r *Registry) Loaded() []Info {
	out := make([]Info, len(r.loaded))
	copy(out, r.loaded)
	return out
}

type CommandHost struct {
	Registry *command.Registry
}

func (h CommandHost) AddCommand(c command.Command) {
	h.Registry.Register(c)
}
