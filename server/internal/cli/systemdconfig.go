package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/config"
)

const runtimeDirName = "laminara"

type systemdOptions struct {
	Binary   string
	Config   string
	User     string
	Group    string
	Writable []string
}

func systemdConfigCmd() *cobra.Command {
	var configPath, binary, user, group string

	cmd := &cobra.Command{
		Use:   "systemd-config",
		Short: "напечатать unit systemd под этот сервер",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			configPath, err := config.Discover(configPath)
			if err != nil {
				return err
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			if binary == "" {
				binary, err = os.Executable()
				if err != nil {
					return fmt.Errorf("не смог определить путь к программе, укажите его сам: --binary /usr/local/bin/laminara-server")
				}
			}
			absConfig, err := filepath.Abs(configPath)
			if err != nil {
				return err
			}
			if group == "" {
				group = user
			}
			opts := systemdOptions{
				Binary:   binary,
				Config:   absConfig,
				User:     user,
				Group:    group,
				Writable: writablePaths(cfg, absConfig, binary),
			}
			fmt.Fprint(cmd.OutOrStdout(), renderSystemd(opts))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().StringVar(&binary, "binary", "", "путь к программе (по умолчанию — та, что печатает unit)")
	cmd.Flags().StringVar(&user, "user", "laminara", "от какого пользователя работает служба")
	cmd.Flags().StringVar(&group, "group", "", "группа службы (по умолчанию — как пользователь)")
	return cmd
}

func writablePaths(cfg *config.Config, configPath, binary string) []string {
	paths := []string{"/run/" + runtimeDirName, filepath.Dir(configPath)}
	add := func(path string) {
		if path = strings.TrimSpace(path); path != "" {
			paths = append(paths, path)
		}
	}
	addFile := func(path string) {
		if path = strings.TrimSpace(path); path != "" {
			add(filepath.Dir(path))
		}
	}

	if cfg.Build != nil {
		add(cfg.Build.ProfilesDir)
		addFile(cfg.Build.SigningKeyPath)
	}
	if cfg.Storage != nil && cfg.Storage.Backend == "fs" {
		var fs struct {
			Root string `json:"root"`
		}
		if json.Unmarshal(cfg.Storage.Config, &fs) == nil {
			add(fs.Root)
		}
	}
	if cfg.Launcher != nil {
		add(cfg.Launcher.Dir)
	}
	if cfg.Yggdrasil != nil {
		addFile(cfg.Yggdrasil.RSAKeyPath)
	}
	if cfg.HWID != nil {
		addFile(cfg.HWID.TicketSecretPath)
		addFile(cfg.HWID.SaltPath)
		if cfg.HWID.Store.Backend != "" && cfg.HWID.Store.Backend != "memory" {
			var store struct {
				DSN string `json:"dsn"`
			}
			if json.Unmarshal(cfg.HWID.Store.Config, &store) == nil {
				addFile(fileOfDSN(store.DSN))
			}
		}
	}
	if cfg.Console != nil {
		addFile(cfg.Console.StatePath)
	}
	if cfg.Log != nil {
		addFile(cfg.Log.File)
	}
	if cfg.Update.Installs() {
		add(filepath.Dir(binary))
	}
	return foldPaths(paths)
}

func fileOfDSN(dsn string) string {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return ""
	}
	dsn = strings.TrimPrefix(dsn, "file:")
	if cut := strings.IndexAny(dsn, "?#"); cut >= 0 {
		dsn = dsn[:cut]
	}
	if !strings.HasPrefix(dsn, "/") {
		return ""
	}
	return dsn
}

func foldPaths(paths []string) []string {
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		if !filepath.IsAbs(path) {
			continue
		}
		cleaned = append(cleaned, filepath.Clean(path))
	}
	sort.Strings(cleaned)

	kept := make([]string, 0, len(cleaned))
	for _, path := range cleaned {
		covered := false
		for _, parent := range kept {
			if path == parent || strings.HasPrefix(path, parent+"/") {
				covered = true
				break
			}
		}
		if !covered {
			kept = append(kept, path)
		}
	}
	return kept
}

func renderSystemd(opts systemdOptions) string {
	var out strings.Builder
	out.WriteString("[Unit]\n")
	out.WriteString("Description=Laminara launcher server\n")
	out.WriteString("Documentation=https://docs.laminara.dev\n")
	out.WriteString("After=network-online.target\n")
	out.WriteString("Wants=network-online.target\n\n")

	out.WriteString("[Service]\n")
	out.WriteString("Type=notify\n")
	fmt.Fprintf(&out, "User=%s\nGroup=%s\n", opts.User, opts.Group)
	fmt.Fprintf(&out, "ExecStart=%s start --config %s\n", opts.Binary, opts.Config)
	fmt.Fprintf(&out, "ExecStop=%s stop\n", opts.Binary)
	out.WriteString("Restart=on-failure\n")
	out.WriteString("RestartSec=5\n")
	out.WriteString("RestartPreventExitStatus=78\n")
	out.WriteString("TimeoutStopSec=30\n\n")

	fmt.Fprintf(&out, "RuntimeDirectory=%s\n", runtimeDirName)
	fmt.Fprintf(&out, "Environment=XDG_RUNTIME_DIR=/run/%s\n\n", runtimeDirName)

	out.WriteString("NoNewPrivileges=true\n")
	out.WriteString("ProtectSystem=strict\n")
	out.WriteString("ProtectHome=true\n")
	out.WriteString("PrivateTmp=true\n")
	fmt.Fprintf(&out, "ReadWritePaths=%s\n\n", strings.Join(opts.Writable, " "))

	out.WriteString("[Install]\n")
	out.WriteString("WantedBy=multi-user.target\n")
	return out.String()
}
