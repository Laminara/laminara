package cli

import (
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/humanize"
)

func statusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "show server status",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			resp, err := adminClient().Status(cmd.Context(), connect.NewRequest(&adminv1.StatusRequest{}))
			if err != nil {
				return err
			}
			msg := resp.Msg
			if asJSON {
				out, err := protojson.Marshal(msg)
				if err != nil {
					return err
				}
				fmt.Println(string(out))
				return nil
			}
			fmt.Printf("версия:   %s\n", msg.Version)
			fmt.Printf("в работе: %s\n", humanize.Duration(time.Since(time.Unix(0, msg.StartedAtUnixNanos))))
			fmt.Printf("модулей:  %d\n", msg.ModulesLoaded)
			fmt.Printf("память:   %s\n", humanize.Bytes(msg.MemoryBytes))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
