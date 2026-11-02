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
	stdout := logbus.Stdout()
	if cfg != nil && cfg.Format == "json" {
		stdout.JSON = true
		stdout.Color = false
	}
	if cfg == nil || cfg.File == "" {
		return []logbus.Target{stdout}, nil
	}

	writer, err := logfile.Open(logfile.Options{
		Path:      cfg.File,
		MaxSizeMB: cfg.MaxSizeMB,
		Keep:      cfg.Keep,
		MaxAge:    cfg.MaxAge.Duration(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "журнал пишется только в консоль: файл %s открыть не удалось (%v)\n", cfg.File, err)
		return []logbus.Target{stdout}, nil
	}
	file := logbus.Plain(writer)
	file.JSON = cfg.Format == "json"
	return []logbus.Target{stdout, file}, writer
}
