package module

import (
	"context"
	"io"
)

type Info struct {
	Name        string
	Version     string
	Description string
}

type Command struct {
	Name     string
	Aliases  []string
	Synopsis string
	Run      func(ctx context.Context, args []string, out io.Writer) error
}

type CommandSpec struct {
	Name     string
	Aliases  []string
	Synopsis string
}

type Module interface {
	Info() Info
	Commands() []Command
}

type Configurable interface {
	Configure(config []byte) error
}

type EventHandler interface {
	Events() []string
	OnEvent(ctx context.Context, topic string, data map[string]string) error
}

type Service interface {
	Configure(ctx context.Context, config []byte) error
	Info(ctx context.Context) (Info, []CommandSpec, []string, error)
	Execute(ctx context.Context, command string, args []string, out io.Writer) error
	Emit(ctx context.Context, topic string, data map[string]string) error
}
