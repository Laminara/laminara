package cli

import (
	"os"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
)

func logsCmd() *cobra.Command {
	var follow bool
	var level string
	var source string
	var backscroll uint32
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "stream server logs",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			stream, err := adminClient().StreamLogs(cmd.Context(), connect.NewRequest(&adminv1.StreamLogsRequest{
				MinLevel:   parseLevel(level),
				Source:     source,
				Backscroll: backscroll,
				Follow:     follow,
			}))
			if err != nil {
				return err
			}
			defer stream.Close()
			for stream.Receive() {
				writeLine(os.Stdout, stream.Msg().Line)
			}
			return stream.Err()
		},
	}
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new lines")
	cmd.Flags().StringVar(&level, "level", "", "minimum level (debug|info|warn|error)")
	cmd.Flags().StringVar(&source, "source", "", "filter by source")
	cmd.Flags().Uint32Var(&backscroll, "backscroll", 200, "number of past lines to show")
	return cmd
}
