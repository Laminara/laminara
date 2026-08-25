package cli

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/backup"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/doctor"
)

func doctorCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "проверить, всё ли на месте до того, как что-то сломается",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			results := doctor.Run(cmd.Context(), cfg, configPath)
			out := cmd.OutOrStdout()
			fmt.Fprint(out, doctor.Format(results))

			switch doctor.Worst(results) {
			case doctor.Fail:
				return errors.New("так сервер работать не будет — почините отмеченное «плохо»")
			case doctor.Warn:
				fmt.Fprintln(out, "\nРаботать будет, но отмеченное «важно» однажды аукнется.")
			default:
				fmt.Fprintln(out, "\nВсё на месте.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func backupCmd() *cobra.Command {
	var configPath string
	var target string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "сохранить ключи, настройки и базу компьютеров в один архив",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if target == "" {
				target = backup.DefaultName(time.Now())
			}
			manifest, err := backup.Create(cfg, configPath, target)
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Сохранено в %s\n\n", target)
			for _, item := range manifest.Items {
				fmt.Fprintf(out, "  %-26s %s\n", item.What, item.Path)
			}
			if len(manifest.Skipped) > 0 {
				fmt.Fprintln(out, "\nВ архив НЕ попало — это восстанавливается пересборкой, а не копией:")
				for _, skipped := range manifest.Skipped {
					fmt.Fprintf(out, "  %s\n", skipped)
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().StringVar(&target, "out", "", "куда положить архив")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func restoreCmd() *cobra.Command {
	var archive string
	var into string
	var force bool
	cmd := &cobra.Command{
		Use:   "restore",
		Short: "разложить обратно файлы из архива сохранения",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			out := cmd.OutOrStdout()
			manifest, err := backup.Read(archive)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Сохранение от %s, версия сервера %s\n\n",
				manifest.CreatedAt.Local().Format("02.01.2006 15:04"), manifest.Version)

			results, err := backup.Restore(archive, into, force)
			if err != nil {
				return err
			}
			skipped := 0
			for _, result := range results {
				if result.Written {
					fmt.Fprintf(out, "  восстановлен  %s\n", result.Item.Path)
					continue
				}
				skipped++
				fmt.Fprintf(out, "  пропущен      %s — %s\n", result.Item.Path, result.Reason)
			}
			if skipped > 0 && !force {
				fmt.Fprintln(out, "\nЧтобы заменить то, что уже лежит на диске, добавьте --force.")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&archive, "archive", "", "архив, сделанный командой backup")
	cmd.Flags().StringVar(&into, "into", "", "разложить внутрь этой папки, а не по исходным путям")
	cmd.Flags().BoolVar(&force, "force", false, "заменять файлы, которые уже есть на диске")
	_ = cmd.MarkFlagRequired("archive")
	return cmd
}
