package logfile

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	DefaultMaxSizeMB = 20
	DefaultKeep      = 5
	DefaultMaxAge    = 30 * 24 * time.Hour
)

type Options struct {
	Path      string
	MaxSizeMB int
	Keep      int
	MaxAge    time.Duration
	Now       func() time.Time
}

type Writer struct {
	options Options

	mu      sync.Mutex
	file    *os.File
	written int64
}

func Open(options Options) (*Writer, error) {
	if options.Path == "" {
		return nil, fmt.Errorf("путь к файлу логов не задан")
	}
	if options.MaxSizeMB <= 0 {
		options.MaxSizeMB = DefaultMaxSizeMB
	}
	if options.Keep <= 0 {
		options.Keep = DefaultKeep
	}
	if options.MaxAge <= 0 {
		options.MaxAge = DefaultMaxAge
	}
	if options.Now == nil {
		options.Now = time.Now
	}

	if err := os.MkdirAll(filepath.Dir(options.Path), 0o755); err != nil {
		return nil, err
	}
	writer := &Writer{options: options}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func (w *Writer) open() error {
	file, err := os.OpenFile(w.options.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o640)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return err
	}
	w.file = file
	w.written = info.Size()
	return nil
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return 0, os.ErrClosed
	}
	if w.written > 0 && w.written+int64(len(p)) > int64(w.options.MaxSizeMB)*1024*1024 {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}

	written, err := w.file.Write(p)
	w.written += int64(written)
	return written, err
}

func (w *Writer) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	stamp := w.options.Now().Format("20060102-150405")
	rotated := rotatedName(w.options.Path, stamp)
	for suffix := 1; ; suffix++ {
		if _, err := os.Stat(rotated); os.IsNotExist(err) {
			break
		}
		rotated = rotatedName(w.options.Path, fmt.Sprintf("%s-%d", stamp, suffix))
	}
	if err := os.Rename(w.options.Path, rotated); err != nil {
		return err
	}
	if err := w.open(); err != nil {
		return err
	}
	w.prune()
	return nil
}

func rotatedName(path, stamp string) string {
	extension := filepath.Ext(path)
	return strings.TrimSuffix(path, extension) + "-" + stamp + extension
}

func (w *Writer) prune() {
	kept := w.rotatedFiles()
	deadline := w.options.Now().Add(-w.options.MaxAge)

	for index, entry := range kept {
		tooMany := index < len(kept)-w.options.Keep
		tooOld := entry.modified.Before(deadline)
		if tooMany || tooOld {
			os.Remove(entry.path)
		}
	}
}

type rotated struct {
	path     string
	modified time.Time
}

func (w *Writer) rotatedFiles() []rotated {
	extension := filepath.Ext(w.options.Path)
	prefix := strings.TrimSuffix(filepath.Base(w.options.Path), extension) + "-"

	entries, err := os.ReadDir(filepath.Dir(w.options.Path))
	if err != nil {
		return nil
	}

	var found []rotated
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasPrefix(name, prefix) || filepath.Ext(name) != extension {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		found = append(found, rotated{path: filepath.Join(filepath.Dir(w.options.Path), name), modified: info.ModTime()})
	}
	sort.Slice(found, func(i, j int) bool { return found[i].path < found[j].path })
	return found
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	w.file = nil
	return err
}
