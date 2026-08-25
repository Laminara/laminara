package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/laminara/laminara/server/internal/access"
	"github.com/laminara/laminara/server/internal/command"
)

func accessCommand(controller *access.Controller, builds func() ([]string, error)) command.Command {
	return command.Command{
		Name:     "access",
		Synopsis: "кто пускается в сборки (access rules | access check <сборка> <игрок> [uuid] | access reload)",
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			if len(args) == 0 {
				args = []string{"rules"}
			}
			switch args[0] {
			case "rules":
				return printRules(controller, builds, out)
			case "check":
				if len(args) < 3 {
					return errors.New("usage: access check <build> <username> [uuid]")
				}
				uuid := ""
				if len(args) > 3 {
					uuid = args[3]
				}
				subject := access.Subject{Username: args[2], UUID: uuid, Subject: args[2]}
				decision := controller.Decide(ctx, args[1], subject)
				verdict := "denied"
				if decision.Allowed {
					verdict = "allowed"
				}
				fmt.Fprintf(out, "%s: %s\n", args[1], verdict)
				if !decision.Allowed {
					fmt.Fprintf(out, "видимость: %s\nпричина: %s\n", visibilityWord(decision.Hidden), decision.Reason)
				}
				return nil
			case "reload":
				controller.Reload()
				fmt.Fprintln(out, "Списки доступа перечитаны.")
				return nil
			default:
				return fmt.Errorf("не знаю подкоманду access %q", args[0])
			}
		},
	}
}

func visibilityWord(hidden bool) string {
	if hidden {
		return "скрыта из списка"
	}
	return "видна в списке"
}

func printRules(controller *access.Controller, builds func() ([]string, error), out io.Writer) error {
	rules := controller.Describe()
	if len(rules) == 0 {
		fmt.Fprintln(out, "Правил доступа нет — все сборки открыты каждому.")
		return nil
	}
	fmt.Fprintf(out, "файлы сборок: %s\n", guardWord(controller.Guarded()))
	for _, rule := range rules {
		fmt.Fprintf(out, "%s — список «%s», %s\n  %s\n", strings.Join(rule.Builds, ", "), rule.Source, visibilityWord(rule.Hidden), rule.Message)
	}
	if builds == nil {
		return nil
	}
	names, err := builds()
	if err != nil {
		return err
	}
	anonymous := access.Subject{}
	fmt.Fprintln(out, "\nсборки:")
	for _, name := range names {
		decision := controller.Decide(context.Background(), name, anonymous)
		state := "открыта всем"
		if !decision.Allowed {
			state = "по списку, " + visibilityWord(decision.Hidden)
		}
		fmt.Fprintf(out, "  %-28s %s\n", name, state)
	}
	return nil
}

func guardWord(guarded bool) string {
	if guarded {
		return "под охраной — скачать может только допущенный игрок"
	}
	return "открыты — файлы скачает любой, у кого есть манифест"
}
