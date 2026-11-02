package cli

import (
	"fmt"
	"os"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/control"
	"github.com/laminara/laminara/server/internal/daemon"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func settingsApplyCmd(configPath *string) *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use: "apply <файл-правок.json>", Short: "проверить и сохранить экспортированные изменения настроек", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			patch := &adminv1.SettingsPatch{}
			if err := protojson.Unmarshal(data, patch); err != nil {
				return fmt.Errorf("файл правок не читается; нужен экспорт из настроек")
			}
			var preview *adminv1.PreviewSettingsResponse
			client := adminClient(cmd.Context())
			if *configPath != "" {
				if patch.TargetBuild != "" {
					return fmt.Errorf("правки сборки применяются через запущенный сервер; уберите --config")
				}
				preview, err = daemon.PreviewSettingsFile(*configPath, patch)
			} else {
				if err := control.RequireCapability(cmd.Context(), client, adminv1.AdminCapability_ADMIN_CAPABILITY_SETTINGS_PATCH); err != nil {
					return err
				}
				preview, err = control.PreviewSettings(cmd.Context(), client, patch.TargetBuild, &adminv1.PreviewSettingsRequest{Patch: patch})
			}
			if err != nil {
				return err
			}
			for _, difference := range preview.Differences {
				fmt.Fprintf(cmd.OutOrStdout(), "%s: %s → %s\n", difference.Path, difference.Before, difference.After)
			}
			if preview.ValidationError != "" {
				return fmt.Errorf("%s", preview.ValidationError)
			}
			if dryRun {
				return nil
			}
			var revision string
			if *configPath != "" {
				revision, err = daemon.ApplySettingsFile(*configPath, patch)
			} else {
				revision, err = control.ApplySettings(cmd.Context(), client, patch)
			}
			if err != nil {
				return err
			}
			if patch.TargetBuild == "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Настройки сохранены; ревизия %s. Для применения нужен перезапуск.\n", revision)
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "Настройки сборки сохранены; ревизия %s. Для применения нужна публикация.\n", revision)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "проверить и показать изменения без записи")
	return cmd
}
