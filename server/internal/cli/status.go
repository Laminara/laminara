package cli

import (
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
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
			uptime := time.Since(time.Unix(0, msg.StartedAtUnixNanos)).Truncate(time.Second)
			fmt.Printf("version:    %s\n", msg.Version)
			fmt.Printf("uptime:     %s\n", uptime)
			fmt.Printf("modules:    %d\n", msg.ModulesLoaded)
			fmt.Printf("memory:     %d KiB\n", msg.MemoryBytes/1024)
			fmt.Printf("goroutines: %d\n", msg.Goroutines)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "output as JSON")
	return cmd
}
