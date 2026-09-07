package daemon

import (
	"fmt"
	"io"
	"os"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/logbus"
	"github.com/laminara/laminara/server/internal/logfile"
)

func logTargets(cfg *config.LogConfig) ([]logbus.Target, io.Closer) {
	if cfg == nil || cfg.File == "" {
		return []logbus.Target{logbus.Stdout()}, nil
	}

	writer, err := logfile.Open(logfile.Options{
		Path:      cfg.File,
		MaxSizeMB: cfg.MaxSizeMB,
		Keep:      cfg.Keep,
		MaxAge:    cfg.MaxAge.Duration(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "журнал пишется только в консоль: файл %s открыть не удалось (%v)\n", cfg.File, err)
		return []logbus.Target{logbus.Stdout()}, nil
	}
	return []logbus.Target{logbus.Stdout(), logbus.Plain(writer)}, writer
}
