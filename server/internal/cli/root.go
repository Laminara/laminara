package cli

import "github.com/spf13/cobra"

func Root() *cobra.Command {
	root := &cobra.Command{
		Use:           "laminara-server",
		Short:         "Laminara server daemon and control client",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		startCmd(),
		stopCmd(),
		statusCmd(),
		consoleCmd(),
		logsCmd(),
		execCmd(),
		hashCmd(),
		clientConfigCmd(),
	)
	return root
}
