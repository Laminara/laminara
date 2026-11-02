package cli

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/control"
)

func execCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "exec [команда…]",
		Short: "выполнить одну команду сервера и выйти",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if asJSON {
				if len(args) == 1 {
					var err error
					args, err = command.Split(args[0])
					if err != nil {
						return err
					}
				}
				code, err := control.Execute(cmd.Context(), adminClient(cmd.Context()), args, func(id string) { fmt.Fprintln(cmd.ErrOrStderr(), "Задание:", id) }, func(event *adminv1.ExecResponse) error { return writeProto(cmd.OutOrStdout(), event) })
				if err != nil {
					return err
				}
				if code != 0 {
					return fmt.Errorf("команда завершилась с кодом %d", code)
				}
				return nil
			}
			request := &adminv1.ExecRequest{Arguments: args}
			if len(args) == 1 {
				request = &adminv1.ExecRequest{Line: args[0]}
			}
			code, err := runExecRequest(cmd.Context(), adminClient(cmd.Context()), request)
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(int(code))
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "события выполнения в NDJSON")
	return cmd
}

func runExec(ctx context.Context, client adminv1connect.AdminServiceClient, line string) (int32, error) {
	return runExecRequest(ctx, client, &adminv1.ExecRequest{Line: line})
}

func runExecRequest(ctx context.Context, client adminv1connect.AdminServiceClient, request *adminv1.ExecRequest) (int32, error) {
	if len(request.Arguments) == 0 {
		arguments, err := command.Split(request.Line)
		if err != nil {
			return 0, err
		}
		request = &adminv1.ExecRequest{Arguments: arguments}
	}
	return control.Execute(ctx, client, request.Arguments, func(id string) {
		fmt.Fprintln(os.Stderr, "Задание:", id)
	}, func(event *adminv1.ExecResponse) error {
		if output := event.GetOutput(); output != nil {
			target := os.Stdout
			if output.Stderr {
				target = os.Stderr
			}
			_, err := fmt.Fprint(target, output.Text)
			return err
		}
		return nil
	})
}
