package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/selfupdate"
	"github.com/laminara/laminara/server/internal/version"
)

func updateCmd() *cobra.Command {
	var configPath string
	var checkOnly bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "обновить сервер до свежего релиза",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			repo := selfupdate.DefaultRepo
			if configPath != "" {
				cfg, err := config.Load(configPath)
				if err != nil {
					return err
				}
				repo = cfg.Update.RepoOr(repo)
			}
			checker := &selfupdate.Checker{Repo: repo}
			return selfupdate.Run(cmd.Context(), checker, version.Current, checkOnly, "laminara-server restart", cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().BoolVar(&checkOnly, "check", false, "только посмотреть, вышла ли версия свежее")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "версия сервера",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintln(cmd.OutOrStdout(), version.Current)
		},
	}
}
