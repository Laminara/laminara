package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	module "github.com/laminara/laminara/sdk/go/module"
)

type greeter struct {
	greeting string
	eventLog string
}

func (g *greeter) Info() module.Info {
	return module.Info{Name: "greeter", Version: "0.1.0", Description: "Пример внешнего модуля"}
}

func (g *greeter) Configure(config []byte) error {
	var settings struct {
		Greeting string `json:"greeting"`
		EventLog string `json:"eventLog"`
	}
	if err := json.Unmarshal(config, &settings); err != nil {
		return err
	}
	if settings.Greeting != "" {
		g.greeting = settings.Greeting
	}
	g.eventLog = settings.EventLog
	return nil
}

func (g *greeter) Commands() []module.Command {
	return []module.Command{
		{
			Name:     "greet",
			Aliases:  []string{"hi"},
			Synopsis: "поприветствовать (пример внешнего модуля)",
			Run: func(_ context.Context, args []string, out io.Writer) error {
				who := "мир"
				if len(args) > 0 {
					who = strings.Join(args, " ")
				}
				fmt.Fprintf(out, "%s, %s! — из внешнего модуля greeter\n", g.greeting, who)
				return nil
			},
		},
	}
}

func (g *greeter) Events() []string {
	return []string{"build.published"}
}

func (g *greeter) OnEvent(_ context.Context, topic string, data map[string]string) error {
	if g.eventLog == "" {
		return nil
	}
	file, err := os.OpenFile(g.eventLog, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = fmt.Fprintf(file, "%s: событие %s (сборка %s)\n", g.greeting, topic, data["name"])
	return err
}

func main() {
	module.Serve(&greeter{greeting: "Привет"})
}
