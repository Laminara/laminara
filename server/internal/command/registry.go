package command

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"
)

type Command struct {
	Name       string
	Aliases    []string
	Synopsis   string
	Secret     bool
	SecretArgs bool
	Run        func(ctx context.Context, args []string, out io.Writer) error
}

type Registry struct {
	commands map[string]Command
	asleep   map[string]string
	observe  func(time.Duration, error)
}

func (r *Registry) SetObserver(observe func(time.Duration, error)) { r.observe = observe }

func NewRegistry() *Registry {
	return &Registry{commands: make(map[string]Command), asleep: make(map[string]string)}
}

func (r *Registry) Sleeping(reason string, names ...string) {
	for _, name := range names {
		if _, taken := r.commands[name]; !taken {
			r.asleep[name] = reason
		}
	}
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

func (r *Registry) Lookup(name string) (Command, bool) {
	c, ok := r.commands[name]
	return c, ok
}

func (r *Registry) Dispatch(ctx context.Context, line string, out io.Writer) error {
	fields, err := Split(line)
	if err != nil {
		return err
	}
	return r.DispatchArgs(ctx, fields, out)
}

func (r *Registry) DispatchArgs(ctx context.Context, fields []string, out io.Writer) (result error) {
	if len(fields) == 0 {
		return nil
	}
	if r.observe != nil {
		start := time.Now()
		defer func() { r.observe(time.Since(start), result) }()
	}
	c, ok := r.commands[fields[0]]
	if !ok {
		if reason, sleeping := r.asleep[fields[0]]; sleeping {
			return fmt.Errorf("команда «%s» есть, но сейчас выключена: %s", fields[0], reason)
		}
		return fmt.Errorf("команды «%s» нет — весь список даёт help", fields[0])
	}
	if err := r.Authorize(ctx, c.Name); err != nil {
		return err
	}
	err := c.Run(ctx, fields[1:], out)
	if policy, ok := ctx.Value(policyKey{}).(Policy); ok {
		policy.Completed(ctx, c, err)
	}
	return err
}

func HelpCommand(r *Registry) Command {
	return Command{
		Name:     "help",
		Synopsis: "список команд",
		Run: func(_ context.Context, _ []string, out io.Writer) error {
			for _, c := range r.List() {
				fmt.Fprintf(out, "%-16s %s\n", c.Name, c.Synopsis)
			}
			return nil
		},
	}
}
