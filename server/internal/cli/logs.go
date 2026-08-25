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
		Short: "смотреть логи сервера",
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
	cmd.Flags().BoolVarP(&follow, "follow", "f", false, "не выходить, показывать новые строки")
	cmd.Flags().StringVar(&level, "level", "", "с какого уровня показывать (debug|info|warn|error)")
	cmd.Flags().StringVar(&source, "source", "", "только строки этого источника")
	cmd.Flags().Uint32Var(&backscroll, "backscroll", 200, "сколько прошлых строк показать")
	return cmd
}
