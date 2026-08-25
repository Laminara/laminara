package daemon

import (
	"fmt"
	"io"
	"os"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/logfile"
)

func logSink(cfg *config.LogConfig) (io.Writer, io.Closer) {
	if cfg == nil || cfg.File == "" {
		return os.Stdout, nil
	}

	writer, err := logfile.Open(logfile.Options{
		Path:      cfg.File,
		MaxSizeMB: cfg.MaxSizeMB,
		Keep:      cfg.Keep,
		MaxAge:    cfg.MaxAge.Duration(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "журнал пишется только в консоль: файл %s открыть не удалось (%v)\n", cfg.File, err)
		return os.Stdout, nil
	}
	return io.MultiWriter(os.Stdout, writer), writer
}
