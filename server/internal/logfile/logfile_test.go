package logfile_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/logfile"
)

func rotatedCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "server-") {
			count++
		}
	}
	return count
}

func TestWritesGoToTheFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")

	writer, err := logfile.Open(logfile.Options{Path: path})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Write([]byte("сервер запущен\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "сервер запущен\n" {
		t.Fatalf("file = %q", body)
	}
}

func TestReopeningAppends(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")

	for i := 0; i < 2; i++ {
		writer, err := logfile.Open(logfile.Options{Path: path})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte("строка\n")); err != nil {
			t.Fatal(err)
		}
		writer.Close()
	}

	body, _ := os.ReadFile(path)
	if strings.Count(string(body), "строка") != 2 {
		t.Fatalf("restart truncated the log: %q", body)
	}
}

func TestFileIsRotatedWhenItGrows(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	moment := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	writer, err := logfile.Open(logfile.Options{
		Path:      path,
		MaxSizeMB: 1,
		Now:       func() time.Time { return moment },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	line := strings.Repeat("x", 512*1024) + "\n"
	for i := 0; i < 3; i++ {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		moment = moment.Add(time.Minute)
	}

	if rotatedCount(t, dir) == 0 {
		t.Fatal("the log grew past its limit and was never rotated")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > 1024*1024 {
		t.Fatalf("current log = %d bytes, want it under the 1 MB limit", info.Size())
	}
}

func TestOldFilesAreDropped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.log")
	moment := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)

	writer, err := logfile.Open(logfile.Options{
		Path:      path,
		MaxSizeMB: 1,
		Keep:      2,
		Now:       func() time.Time { return moment },
	})
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()

	line := strings.Repeat("x", 700*1024) + "\n"
	for i := 0; i < 6; i++ {
		if _, err := writer.Write([]byte(line)); err != nil {
			t.Fatal(err)
		}
		moment = moment.Add(time.Hour)
	}

	if got := rotatedCount(t, dir); got > 2 {
		t.Fatalf("kept %d rotated files, want at most 2", got)
	}
}

func TestPathIsRequired(t *testing.T) {
	if _, err := logfile.Open(logfile.Options{}); err == nil {
		t.Fatal("opening without a path must fail rather than write somewhere unexpected")
	}
}
