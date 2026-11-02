package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"connectrpc.com/connect"
	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/control"
)

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "остановить сервер",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if remoteClient(cmd.Context()) {
				_, err := adminClient(cmd.Context()).Stop(cmd.Context(), connect.NewRequest(&adminv1.StopRequest{}))
				return err
			}
			data, err := os.ReadFile(control.PidPath())
			if err != nil {
				return fmt.Errorf("server does not appear to be running: %w", err)
			}
			pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
			if err != nil {
				return fmt.Errorf("invalid pid file: %w", err)
			}
			proc, err := os.FindProcess(pid)
			if err != nil {
				return err
			}
			return proc.Signal(syscall.SIGTERM)
		},
	}
}
