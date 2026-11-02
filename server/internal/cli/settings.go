package cli

import (
	"fmt"
	"net"
	"os"
	"time"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/control"
	"github.com/laminara/laminara/server/internal/settings"
)

func settingsCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "settings <путь> [значение]",
		Short: "показать или изменить настройку",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if configPath == "" && (remoteClient(cmd.Context()) || daemonAnswers()) {
				code, err := runExecRequest(cmd.Context(), adminClient(cmd.Context()), &adminv1.ExecRequest{Arguments: append([]string{"settings"}, args...)})
				if err != nil {
					return err
				}
				if code != 0 {
					os.Exit(int(code))
				}
				return nil
			}
			if configPath == "" {
				return fmt.Errorf("сервер не запущен, поэтому настройку надо менять прямо в файле — укажите его: --config <путь>")
			}
			doc, err := settings.Open(configPath)
			if err != nil {
				return err
			}
			if len(args) == 1 {
				entry, err := doc.Entry(args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", entry.Label, entry.Display)
				return nil
			}
			if err := doc.Set(args[0], args[1]); err != nil {
				return err
			}
			if err := doc.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Записано: %s\n", args[0])
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&configPath, "config", "", "путь к конфигу для прямой правки файла")
	cmd.AddCommand(settingsApplyCmd(&configPath))
	return cmd
}

func daemonAnswers() bool {
	conn, err := net.DialTimeout("unix", control.SocketPath(), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
