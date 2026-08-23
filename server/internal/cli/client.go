package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/gen/go/laminara/admin/v1/adminv1connect"
	"github.com/laminara/laminara/server/internal/control"
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
	timestamp := time.Unix(0, line.TimeUnixNanos).Format("15:04:05")
	source := line.Source
	if source == "" {
		source = "-"
	}
	fmt.Fprintf(w, "%s %-5s %s: %s", timestamp, levelName(line.Level), source, line.Message)
	keys := make([]string, 0, len(line.Fields))
	for k := range line.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(w, " %s=%s", k, line.Fields[k])
	}
	fmt.Fprintln(w)
}
