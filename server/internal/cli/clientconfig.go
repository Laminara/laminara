package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/clientconfig"
	"github.com/laminara/laminara/server/internal/config"
)

func clientConfigCmd() *cobra.Command {
	var configPath string
	var endpoints []string
	cmd := &cobra.Command{
		Use:   "client-config",
		Short: "напечатать конфигурацию, которую запекают в лаунчер: адреса, ключи подписи, оформление",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if len(endpoints) == 0 && cfg.Launcher != nil {
				endpoints = cfg.Launcher.Endpoints
			}
			if len(endpoints) == 0 {
				if cfg.API == nil || cfg.API.Addr == "" {
					return fmt.Errorf("не указан --endpoint, и в конфиге нет ни launcher.endpoints, ни api.addr")
				}
				endpoints = []string{"http://" + cfg.API.Addr}
				fmt.Fprintln(os.Stderr, "Внимание: адрес не указан — беру api.addr, а это почти наверняка не тот адрес, по которому придут игроки. Укажите --endpoint или launcher.endpoints")
			}
			document, err := clientconfig.Build(cfg, endpoints)
			if err != nil {
				return err
			}
			data, err := document.JSON()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), string(data))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().StringArrayVar(&endpoints, "endpoint", nil, "публичный адрес, по которому придут игроки (можно несколько раз, по порядку предпочтения)")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}
