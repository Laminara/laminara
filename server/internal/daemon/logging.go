package daemon

import (
	"io"
	"log/slog"

	"github.com/laminara/laminara/server/internal/config"
	"github.com/laminara/laminara/server/internal/logbus"
)

type Logging struct {
	Log   *slog.Logger
	Bus   *logbus.Bus
	Level *slog.LevelVar
	File  io.Closer
}

func NewLogging(cfg *config.LogConfig) *Logging {
	bus := logbus.NewBus(4096)
	level := new(slog.LevelVar)
	level.Set(slog.LevelInfo)
	sink, file := logSink(cfg)
	log := slog.New(logbus.NewHandler(sink, level, bus))
	slog.SetDefault(log)
	return &Logging{Log: log, Bus: bus, Level: level, File: file}
}
