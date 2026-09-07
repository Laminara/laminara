package daemon

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/doctor"
)

func doctorCommand(opts Options) command.Command {
	return command.Command{
		Name:       "doctor",
		Aliases:    []string{"check"},
		Synopsis:   "doctor [раздел…] [as=<ник> pass=<пароль>] [fix] [deep] — проверить, что всё настроено и работает",
		SecretArgs: true,
		Run: func(ctx context.Context, args []string, out io.Writer) error {
			request := parseDoctorArgs(args)
			results := doctor.RunSections(ctx, doctor.Options{
				Config:     opts.Config,
				ConfigPath: opts.ConfigPath,
				Wired:      opts.Wired,
				Running:    true,
				Username:   request.username,
				Password:   request.password,
				Deep:       request.deep,
			}, request.sections)
			doctor.Write(out, results)
			if request.fix {
				fmt.Fprintln(out)
				doctor.Fix(ctx, out, results)
			}
			if diag.Worst(results) == diag.Fail {
				return fmt.Errorf("проверка нашла то, из-за чего сервер не работает как надо")
			}
			return nil
		},
	}
}

type doctorRequest struct {
	sections []string
	username string
	password string
	fix      bool
	deep     bool
}

func parseDoctorArgs(args []string) doctorRequest {
	known := map[string]bool{}
	for _, name := range doctor.Sections() {
		known[name] = true
	}
	var request doctorRequest
	for _, arg := range args {
		switch {
		case strings.HasPrefix(arg, "as="):
			request.username = strings.TrimPrefix(arg, "as=")
		case strings.HasPrefix(arg, "pass="):
			request.password = strings.TrimPrefix(arg, "pass=")
		case arg == "fix":
			request.fix = true
		case arg == "deep":
			request.deep = true
		case known[arg]:
			request.sections = append(request.sections, arg)
		}
	}
	return request
}
