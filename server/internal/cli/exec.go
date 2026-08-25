package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
)

func execCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "exec [команда…]",
		Short: "выполнить одну команду сервера и выйти",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			code, err := runExec(cmd.Context(), adminClient(), strings.Join(args, " "))
			if err != nil {
				return err
			}
			if code != 0 {
				os.Exit(int(code))
			}
			return nil
		},
	}
}

func runExec(ctx context.Context, client adminv1connect.AdminServiceClient, line string) (int32, error) {
	stream, err := client.Exec(ctx, connect.NewRequest(&adminv1.ExecRequest{Line: line}))
	if err != nil {
		return 0, err
	}
	defer stream.Close()
	var code int32
	for stream.Receive() {
		switch event := stream.Msg().Event.(type) {
		case *adminv1.ExecResponse_Output:
			target := os.Stdout
			if event.Output.Stderr {
				target = os.Stderr
			}
			fmt.Fprint(target, event.Output.Text)
		case *adminv1.ExecResponse_Result:
			code = event.Result.ExitCode
		}
	}
	return code, stream.Err()
}
