package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/laminara/laminara/server/internal/backup"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/diag"
	"github.com/laminara/laminara/server/internal/doctor"
	"github.com/laminara/laminara/server/internal/serversetup"
)

func doctorCmd() *cobra.Command {
	var configPath string
	var username string
	var password string
	var endpoint string
	var only []string
	var asJSON bool
	var fix bool
	var deep bool
	cmd := &cobra.Command{
		Use:   "doctor [раздел…]",
		Short: "проверить, что всё настроено и работает",
		Long: "Проходит по разделам настроек и проверяет каждый в деле: базы отвечают, файлы\n" +
			"заливаются и скачиваются, ключи на месте, подпись сходится. С флагом --as\n" +
			"дополнительно проходит весь путь игрока — вход, продление, список сборок,\n" +
			"манифест, скачивание файла и вход в игре.\n\n" +
			"Разделы: " + strings.Join(doctor.Sections(), " "),
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if username != "" && password == "" {
				password, err = askPassword(cmd, username)
				if err != nil {
					return err
				}
			}
			opts := doctor.Options{
				Config:     cfg,
				ConfigPath: configPath,
				Username:   username,
				Password:   password,
				Endpoint:   endpoint,
				Deep:       deep,
			}
			quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
			previous := slog.Default()
			slog.SetDefault(quiet)
			wired, buildErr := serversetup.Build(cfg)
			slog.SetDefault(previous)
			opts.Wired = wired
			opts.BuildError = buildErr
			opts.Running = serverRunning(cfg)

			results := doctor.RunSections(cmd.Context(), opts, append(only, args...))
			if asJSON {
				encoder := json.NewEncoder(out)
				encoder.SetIndent("", "  ")
				if err := encoder.Encode(doctor.JSON(results)); err != nil {
					return err
				}
			} else {
				doctor.Write(out, results)
			}
			if fix {
				fmt.Fprintln(out)
				doctor.Fix(cmd.Context(), out, results)
			}
			if doctor.Worst(results) == diag.Fail {
				cmd.SilenceUsage = true
				return errors.New("проверка нашла то, из-за чего сервер не будет работать как надо")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().StringVar(&username, "as", "", "пройти путь игрока под этим ником")
	cmd.Flags().StringVar(&password, "password", "", "пароль к --as (по умолчанию спрашивается скрытым вводом)")
	cmd.Flags().StringVar(&endpoint, "endpoint", "", "адрес, по которому обращаться к серверу (по умолчанию берётся из настроек)")
	cmd.Flags().StringSliceVar(&only, "only", nil, "проверить только эти разделы")
	cmd.Flags().BoolVar(&asJSON, "json", false, "вывести результат как JSON")
	cmd.Flags().BoolVar(&fix, "fix", false, "исправить то, что чинится безопасно")
	cmd.Flags().BoolVar(&deep, "deep", false, "проверять все файлы сборок, а не выборку")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func askPassword(cmd *cobra.Command, username string) (string, error) {
	fmt.Fprintf(cmd.ErrOrStderr(), "Пароль для %s: ", username)
	secret, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(cmd.ErrOrStderr())
	if err != nil {
		return "", fmt.Errorf("пароль не прочитан: %w", err)
	}
	return strings.TrimSpace(string(secret)), nil
}

func serverRunning(cfg *config.Config) bool {
	if cfg.API == nil || cfg.API.Addr == "" {
		return false
	}
	conn, err := net.DialTimeout("tcp", dialableAddr(cfg.API.Addr), 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func dialableAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
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
