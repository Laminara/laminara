package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"path"
	"strings"

	"github.com/spf13/cobra"

	"github.com/laminara/laminara/server/internal/config"
)

type nginxOptions struct {
	Domains  []string
	Upstream string
	Objects  string
	Prefix   string
	TLS      bool
}

func nginxConfigCmd() *cobra.Command {
	var configPath string
	var domains []string
	var noTLS bool

	cmd := &cobra.Command{
		Use:   "nginx-config",
		Short: "напечатать конфиг nginx под этот сервер",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if configPath == "" {
				return fmt.Errorf("укажите конфиг сервера: --config <путь>")
			}
			if len(domains) == 0 {
				return fmt.Errorf("укажите домен проекта: --domain launcher.example.com")
			}
			cfg, err := config.Load(configPath)
			if err != nil {
				return err
			}
			opts, err := nginxOptionsOf(cfg, domains, !noTLS)
			if err != nil {
				return err
			}
			fmt.Fprint(cmd.OutOrStdout(), renderNginx(opts))
			return nil
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "путь к конфигу сервера")
	cmd.Flags().StringArrayVar(&domains, "domain", nil, "домен проекта — по нему лаунчер ходит на сервер (можно несколько раз)")
	cmd.Flags().BoolVar(&noTLS, "no-tls", false, "без TLS: один блок на :80, сертификаты держит кто-то другой")
	return cmd
}

func nginxOptionsOf(cfg *config.Config, domains []string, tls bool) (nginxOptions, error) {
	if cfg.API == nil || cfg.API.Addr == "" {
		return nginxOptions{}, fmt.Errorf("в конфиге нет api.addr — публичный слушатель выключен, проксировать нечего")
	}
	opts := nginxOptions{Domains: domains, Upstream: upstreamAddr(cfg.API.Addr), TLS: tls}
	if cfg.API.XAccel && cfg.Storage != nil && cfg.Storage.Backend == "fs" {
		var fs struct {
			Root         string `json:"root"`
			XAccelPrefix string `json:"xaccelPrefix"`
		}
		if len(cfg.Storage.Config) > 0 {
			if err := json.Unmarshal(cfg.Storage.Config, &fs); err != nil {
				return nginxOptions{}, err
			}
		}
		if fs.Root == "" {
			return nginxOptions{}, fmt.Errorf("api.xAccel включён, но у файлового хранилища не задан root")
		}
		prefix := fs.XAccelPrefix
		if prefix == "" {
			prefix = "/_objects"
		}
		opts.Objects = strings.TrimSuffix(fs.Root, "/") + "/"
		opts.Prefix = path.Clean("/"+strings.Trim(prefix, "/")) + "/"
	}
	return opts, nil
}

func upstreamAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::", "[::]":
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port)
}

func renderNginx(opts nginxOptions) string {
	names := strings.Join(opts.Domains, " ")
	var out strings.Builder
	fmt.Fprintf(&out, "upstream laminara {\n    server %s;\n    keepalive 32;\n}\n\n", opts.Upstream)

	if opts.TLS {
		fmt.Fprintf(&out, "server {\n    listen 80;\n    server_name %s;\n    location /.well-known/acme-challenge/ { root /var/www/certbot; }\n    location / { return 301 https://$host$request_uri; }\n}\n\n", names)
		fmt.Fprintf(&out, "server {\n    listen 443 ssl;\n    http2 off;\n    server_name %s;\n\n    ssl_certificate     /etc/letsencrypt/live/%s/fullchain.pem;\n    ssl_certificate_key /etc/letsencrypt/live/%s/privkey.pem;\n\n", names, opts.Domains[0], opts.Domains[0])
	} else {
		fmt.Fprintf(&out, "server {\n    listen 80;\n    server_name %s;\n\n", names)
	}

	out.WriteString("    client_max_body_size 0;\n\n")
	out.WriteString("    location / {\n        proxy_pass http://laminara;\n        proxy_http_version 1.1;\n        proxy_set_header Host $host;\n        proxy_set_header X-Real-IP $remote_addr;\n        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n        proxy_set_header X-Forwarded-Proto $scheme;\n        proxy_buffering off;\n        proxy_request_buffering off;\n    }\n")
	if opts.Objects != "" {
		fmt.Fprintf(&out, "\n    location %s {\n        internal;\n        alias %s;\n        add_header Cache-Control \"public, immutable, max-age=31536000\";\n    }\n", opts.Prefix, opts.Objects)
	}
	out.WriteString("}\n")
	return out.String()
}
