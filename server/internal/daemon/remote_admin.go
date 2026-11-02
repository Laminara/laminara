package daemon

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/controlauth"
)

func (d *Daemon) remoteAdmin(handler http.Handler) (*http.Server, net.Listener, error) {
	cfg := d.adminConfig
	if cfg == nil || cfg.Addr == "" {
		return nil, nil, nil
	}
	if err := config.Validate(&config.Config{Admin: cfg}); err != nil {
		return nil, nil, err
	}
	var security *tls.Config
	if cfg.TLSCert != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCert, cfg.TLSKey)
		if err != nil {
			return nil, nil, err
		}
		security = &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}
	}
	listener, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return nil, nil, err
	}
	if security != nil {
		listener = tls.NewListener(listener, security)
	}
	return &http.Server{Handler: d.tokens.Handler(handler), TLSConfig: security, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute, MaxHeaderBytes: 32 << 10}, listener, nil
}

func tokenCommand(store *controlauth.Store) command.Command {
	return command.Command{Name: "admin-token", Secret: true, Synopsis: "токены администраторов: create <имя> <права,через,запятую> [срок] | list | revoke <id>", Run: func(_ context.Context, args []string, out io.Writer) error {
		if len(args) == 0 {
			return fmt.Errorf("укажите create, list или revoke")
		}
		switch args[0] {
		case "list":
			if len(args) != 1 {
				return fmt.Errorf("admin-token list не принимает аргументы")
			}
			tokens, err := store.List()
			if err != nil {
				return err
			}
			return json.NewEncoder(out).Encode(tokens)
		case "create":
			if len(args) < 3 || len(args) > 4 {
				return fmt.Errorf("admin-token create <имя> <права,через,запятую> [720h]")
			}
			ttl := 30 * 24 * time.Hour
			if len(args) == 4 {
				parsed, err := time.ParseDuration(args[3])
				if err != nil {
					return err
				}
				ttl = parsed
			}
			token, raw, err := store.Create(args[1], strings.Split(args[2], ","), ttl)
			if err != nil {
				return err
			}
			return json.NewEncoder(out).Encode(struct {
				Token  controlauth.Token `json:"metadata"`
				Secret string            `json:"token"`
			}{token, raw})
		case "revoke":
			if len(args) != 2 {
				return fmt.Errorf("admin-token revoke <id>")
			}
			return store.Revoke(args[1])
		default:
			return fmt.Errorf("укажите create, list или revoke")
		}
	}}
}
