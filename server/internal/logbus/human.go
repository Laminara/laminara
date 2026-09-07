package logbus

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
)

const (
	reset = "\x1b[0m"
	dim   = "\x1b[2m"
	bold  = "\x1b[1m"
	red   = "\x1b[31m"
	amber = "\x1b[33m"
	blue  = "\x1b[36m"
)

type Target struct {
	Writer io.Writer
	Color  bool
	Dated  bool
}

func Stdout() Target {
	return Target{Writer: os.Stdout, Color: colorAllowed()}
}

func Plain(w io.Writer) Target {
	return Target{Writer: w, Dated: true}
}

func colorAllowed() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return strings.ToLower(os.Getenv("LAMINARA_COLOR")) != "off"
}

type printer struct {
	mu      sync.Mutex
	targets []Target
}

func (p *printer) write(line Line) {
	rendered := map[[2]bool]string{}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, target := range p.targets {
		key := [2]bool{target.Color, target.Dated}
		text, ready := rendered[key]
		if !ready {
			text = render(line, target.Color, target.Dated)
			rendered[key] = text
		}
		fmt.Fprintln(target.Writer, text)
	}
}

func Render(line Line, color, dated bool) string {
	return render(line, color, dated)
}

func ColorAllowed() bool {
	return colorAllowed()
}

func render(line Line, color, dated bool) string {
	paint := func(text, style string) string {
		if !color || text == "" || style == "" {
			return text
		}
		return style + text + reset
	}

	var out strings.Builder
	stamp := "15:04:05"
	if dated {
		stamp = "02.01 15:04:05"
	}
	out.WriteString(paint(line.Time.Format(stamp), dim))
	out.WriteString(" ")

	if mark, style := levelMark(line.Level); mark != "" {
		out.WriteString(paint(mark, style))
		out.WriteString(" ")
	}

	if line.Source != "" && line.Source != "console" {
		out.WriteString(paint(line.Source, blue))
		out.WriteString(paint(": ", dim))
	}
	out.WriteString(paint(line.Message, messageStyle(line.Level)))

	for _, field := range ordered(line.Fields) {
		out.WriteString("  ")
		out.WriteString(paint(field.key+"=", dim))
		out.WriteString(field.value)
	}
	return out.String()
}

func levelMark(level slog.Level) (string, string) {
	switch {
	case level >= slog.LevelError:
		return "ошибка", red
	case level >= slog.LevelWarn:
		return "важно", amber
	case level <= slog.LevelDebug:
		return "отладка", dim
	default:
		return "", ""
	}
}

func messageStyle(level slog.Level) string {
	if level >= slog.LevelError {
		return bold
	}
	return ""
}

type field struct {
	key   string
	value string
}

func ordered(fields map[string]string) []field {
	if len(fields) == 0 {
		return nil
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	list := make([]field, 0, len(keys))
	for _, key := range keys {
		list = append(list, field{key: key, value: quoteIfNeeded(fields[key])})
	}
	return list
}

func quoteIfNeeded(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\"") {
		return fmt.Sprintf("%q", value)
	}
	return value
}
