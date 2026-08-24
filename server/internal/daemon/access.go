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
					fmt.Fprintf(out, "visibility: %s\nreason: %s\n", visibilityWord(decision.Hidden), decision.Reason)
				}
				return nil
			case "reload":
				controller.Reload()
				fmt.Fprintln(out, "access sources reloaded")
				return nil
			default:
				return fmt.Errorf("unknown access subcommand %q", args[0])
			}
		},
	}
}

func visibilityWord(hidden bool) string {
	if hidden {
		return "hidden"
	}
	return "listed"
}

func printRules(controller *access.Controller, builds func() ([]string, error), out io.Writer) error {
	rules := controller.Describe()
	if len(rules) == 0 {
		fmt.Fprintln(out, "no access rules: every build is public")
		return nil
	}
	fmt.Fprintf(out, "objects: %s\n", guardWord(controller.Guarded()))
	for _, rule := range rules {
		fmt.Fprintf(out, "%s -> source %s (%s)\n  %s\n", strings.Join(rule.Builds, ", "), rule.Source, visibilityWord(rule.Hidden), rule.Message)
	}
	if builds == nil {
		return nil
	}
	names, err := builds()
	if err != nil {
		return err
	}
	anonymous := access.Subject{}
	fmt.Fprintln(out, "\nbuilds:")
	for _, name := range names {
		decision := controller.Decide(context.Background(), name, anonymous)
		state := "public"
		if !decision.Allowed {
			state = "gated (" + visibilityWord(decision.Hidden) + ")"
		}
		fmt.Fprintf(out, "  %-28s %s\n", name, state)
	}
	return nil
}

func guardWord(guarded bool) string {
	if guarded {
		return "guarded (downloads require an allowed session)"
	}
	return "public (anyone holding a manifest can download the files)"
}
