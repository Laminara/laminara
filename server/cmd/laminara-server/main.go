package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/laminara/laminara/server/internal/cli"
	"github.com/laminara/laminara/server/internal/config"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := cli.Root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "laminara-server:", err)
		var broken *config.BadConfigError
		if errors.As(err, &broken) {
			fmt.Fprintln(os.Stderr, "Перезапуск не поможет: поправьте настройку и запустите сервер снова.")
			os.Exit(config.ExitBadConfig)
		}
		os.Exit(1)
	}
}
