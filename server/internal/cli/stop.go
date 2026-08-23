package cli

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/control"
)

func stopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "stop the running server",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
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
