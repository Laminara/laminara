package cli

import (
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/control"
	"github.com/laminara/laminara/server/internal/logbus"
)

func adminClient() adminv1connect.AdminServiceClient {
	return adminv1connect.NewAdminServiceClient(control.HTTPClient(), control.BaseURL())
}

func parseLevel(name string) adminv1.LogLevel {
	switch strings.ToLower(name) {
	case "debug":
		return adminv1.LogLevel_LOG_LEVEL_DEBUG
	case "info":
		return adminv1.LogLevel_LOG_LEVEL_INFO
	case "warn", "warning":
		return adminv1.LogLevel_LOG_LEVEL_WARN
	case "error":
		return adminv1.LogLevel_LOG_LEVEL_ERROR
	default:
		return adminv1.LogLevel_LOG_LEVEL_UNSPECIFIED
	}
}

func levelName(level adminv1.LogLevel) string {
	switch level {
	case adminv1.LogLevel_LOG_LEVEL_DEBUG:
		return "DEBUG"
	case adminv1.LogLevel_LOG_LEVEL_INFO:
		return "INFO"
	case adminv1.LogLevel_LOG_LEVEL_WARN:
		return "WARN"
	case adminv1.LogLevel_LOG_LEVEL_ERROR:
		return "ERROR"
	default:
		return "?"
	}
}

func writeLine(w io.Writer, line *adminv1.LogLine) {
	fmt.Fprintln(w, logbus.Render(logbus.Line{
		Time:    time.Unix(0, line.TimeUnixNanos),
		Level:   levelOf(line.Level),
		Source:  line.Source,
		Message: line.Message,
		Fields:  line.Fields,
	}, logbus.ColorAllowed(), false))
}

func levelOf(level adminv1.LogLevel) slog.Level {
	switch level {
	case adminv1.LogLevel_LOG_LEVEL_DEBUG:
		return slog.LevelDebug
	case adminv1.LogLevel_LOG_LEVEL_WARN:
		return slog.LevelWarn
	case adminv1.LogLevel_LOG_LEVEL_ERROR:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
