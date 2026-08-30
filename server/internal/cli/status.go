package cli

import (
	"fmt"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/buildview"
)

func statusCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "status",
		Short: "состояние сервера",
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
			buildview.WriteStatus(cmd.OutOrStdout(), buildview.Status{
				Version:            msg.Version,
				StartedAtUnixNanos: msg.StartedAtUnixNanos,
				ModulesLoaded:      msg.ModulesLoaded,
				MemoryBytes:        msg.MemoryBytes,
			})
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "вывести JSON")
	return cmd
}
