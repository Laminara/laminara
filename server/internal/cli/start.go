package cli

import (
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/daemon"
	"github.com/laminara/laminara/server/internal/modulesetup"
	"github.com/laminara/laminara/server/internal/serversetup"
	"github.com/laminara/laminara/server/internal/tempdir"
)

func startCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "start",
		Short: "запустить сервер в переднем плане (в фон его уводят systemd или docker)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var opts daemon.Options
			opts.ConfigPath = configPath
			if configPath != "" {
				cfg, err := config.LoadChecked(configPath)
				if err != nil {
					return config.Fatal(err)
				}
				logging := daemon.NewLogging(cfg.Log)
				opts.Logging = logging
				useScratchDir(cfg, configPath, logging.Log)
				opts.Modules = modulesetup.Build(cfg.Modules, logging.Log)
				wired, err := serversetup.Build(cfg)
				if err != nil {
					return config.Fatal(err)
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
				opts.Console = wired.Console
				opts.Update = cfg.Update
				opts.Log = cfg.Log
				opts.Config = cfg
				opts.Wired = wired
			}
			server := daemon.New(opts)
			err := server.Run(cmd.Context())
			if server.Restarting() {
				return relaunch()
			}
			return err
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	return cmd
}

func useScratchDir(cfg *config.Config, configPath string, log *slog.Logger) {
	dir, moved, err := tempdir.Ensure(tempdir.Candidates(cfg, configPath)...)
	if err != nil {
		log.Error("временные файлы негде держать",
			"source", "server",
			"каталог", dir,
			"ошибка", err,
			"что делать", "дайте серверу право писать во временный каталог: в unit systemd это PrivateTmp=true (см. laminara-server systemd-config)",
		)
		return
	}
	if moved {
		log.Warn("временный каталог системы только для чтения",
			"source", "server",
			"пишу в", dir,
			"что делать", "перепишите unit командой laminara-server systemd-config — в нём есть PrivateTmp=true",
		)
	}
}
