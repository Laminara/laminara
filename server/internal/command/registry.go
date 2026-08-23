package command

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
)

type Command struct {
	Name     string
	Aliases  []string
	Synopsis string
	Run      func(ctx context.Context, args []string, out io.Writer) error
}

type Registry struct {
	commands map[string]Command
}

func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command)}
}

func (r *Registry) Register(c Command) {
	r.commands[c.Name] = c
	for _, alias := range c.Aliases {
		r.commands[alias] = c
	}
}

func (r *Registry) List() []Command {
	seen := make(map[string]bool)
	out := make([]Command, 0, len(r.commands))
	for _, c := range r.commands {
		if seen[c.Name] {
			continue
		}
		seen[c.Name] = true
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func (r *Registry) Dispatch(ctx context.Context, line string, out io.Writer) error {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return nil
	}
	c, ok := r.commands[fields[0]]
	if !ok {
		return fmt.Errorf("unknown command %q (try \"help\")", fields[0])
	}
	return c.Run(ctx, fields[1:], out)
}

func HelpCommand(r *Registry) Command {
	return Command{
		Name:     "help",
		Synopsis: "list available commands",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			for _, c := range r.List() {
				fmt.Fprintf(out, "%-16s %s\n", c.Name, c.Synopsis)
			}
			return nil
		},
	}
}
