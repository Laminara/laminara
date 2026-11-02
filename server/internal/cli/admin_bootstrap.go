package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/controlauth"
	"github.com/spf13/cobra"
)

func adminBootstrapCmd() *cobra.Command {
	var configPath, output, name string
	var ttl time.Duration
	cmd := &cobra.Command{Use: "admin-bootstrap", Short: "создать первый административный токен локально", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		cfg, err := config.LoadChecked(configPath)
		if err != nil {
			return err
		}
		if cfg.Admin == nil || cfg.Admin.TokensPath == "" {
			return fmt.Errorf("задайте раздел admin в конфиге")
		}
		if output == "" {
			return fmt.Errorf("укажите --out для файла токена")
		}
		file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		store := controlauth.NewStore(cfg.Admin.TokensPath)
		token, raw, err := store.Bootstrap(name, ttl)
		if err != nil {
			file.Close()
			os.Remove(output)
			return err
		}
		if _, err := fmt.Fprintln(file, raw); err != nil {
			file.Close()
			store.Revoke(token.ID)
			os.Remove(output)
			return err
		}
		if err := file.Sync(); err != nil {
			file.Close()
			store.Revoke(token.ID)
			os.Remove(output)
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Токен %s записан в %s; действует до %s.\n", token.ID, output, token.Expires.Format(time.RFC3339))
		return nil
	}}
	cmd.Flags().StringVar(&configPath, "config", "", "конфиг сервера")
	cmd.Flags().StringVar(&output, "out", "", "новый файл токена с правами 0600")
	cmd.Flags().StringVar(&name, "name", "bootstrap", "название токена")
	cmd.Flags().DurationVar(&ttl, "ttl", 24*time.Hour, "срок действия первого токена")
	return cmd
}
