package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/laminara/laminara/server/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := cli.Root().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "laminara-server:", err)
		os.Exit(1)
	}
}
