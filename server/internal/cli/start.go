package cli

import (
	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/daemon"
	"github.com/laminara/laminara/server/internal/serversetup"
)

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "run the server in the foreground (use systemd or docker to daemonize)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var opts daemon.Options
			if configPath != "" {
				cfg, err := config.Load(configPath)
				if err != nil {
					return err
				}
				wired, err := serversetup.Build(cfg)
				if err != nil {
					return err
				}
				opts.Auth = wired.Auth
				opts.Build = wired.Build
				opts.PublicHandler = wired.PublicHandler
				opts.PublicAddr = wired.PublicAddr
				opts.Events = wired.Events
				opts.Launcher = wired.Launcher
				opts.Access = wired.Access
				opts.Catalog = wired.Catalog
				opts.Machines = wired.Machines
				opts.Signing = wired.Signing
				if cfg.Modules != nil {
					opts.ModulesDir = cfg.Modules.Dir
					if len(cfg.Modules.Config) > 0 {
						opts.ModulesConfig = make(map[string][]byte, len(cfg.Modules.Config))
						for name, raw := range cfg.Modules.Config {
							opts.ModulesConfig[name] = raw
						}
					}
				}
			}
			return daemon.New(opts).Run(cmd.Context())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to a JSON config file")
	return cmd
}
