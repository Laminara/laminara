package cli

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/tui"
)

func consoleCmd() *cobra.Command {
	var nerd bool
	cmd := &cobra.Command{
		Use:   "console",
		Short: "attach the interactive console (live logs + build wizards)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return tui.Run(cmd.Context(), adminClient(), nerd || os.Getenv("LAMINARA_NERD_FONT") != "")
		},
	}
	cmd.Flags().BoolVar(&nerd, "nerd", false, "use Nerd Font icons (requires a Nerd Font in your terminal)")
	return cmd
}
