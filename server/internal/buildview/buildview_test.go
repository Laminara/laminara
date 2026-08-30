package buildview

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteStatusPrintsTheSameLinesForConsoleAndClient(t *testing.T) {
	started := time.Now().Add(-90 * time.Minute)
	var out bytes.Buffer
	WriteStatus(&out, Status{Version: "1.1.0", StartedAtUnixNanos: started.UnixNano(), ModulesLoaded: 2, MemoryBytes: 1536})

	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("status is four lines, got %d: %q", len(lines), out.String())
	}
	if lines[0] != "версия:   1.1.0" {
		t.Fatalf("version line: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "в работе: 1 ч ") {
		t.Fatalf("uptime must come from the start time, got %q", lines[1])
	}
	if lines[2] != "модулей:  2" {
		t.Fatalf("modules line: %q", lines[2])
	}
	if lines[3] != "память:   1,5 КБ" {
		t.Fatalf("memory line: %q", lines[3])
	}
}
