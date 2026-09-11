package prepare

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/laminara/laminara/server/internal/progress"
	"github.com/laminara/laminara/server/internal/safepath"
)

type digest struct {
	algorithm string
	sum       string
}

func sha1Digest(sum string) digest { return digest{algorithm: "sha1", sum: sum} }

func sha256Digest(sum string) digest { return digest{algorithm: "sha256", sum: sum} }

func (d digest) hasher() hash.Hash {
	if d.algorithm == "sha256" {
		return sha256.New()
	}
	return sha1.New()
}

type job struct {
	url        string
	path       string
	digest     digest
	executable bool
}

const perHostLimit = 8

type downloader struct {
	http    *http.Client
	root    string
	workers int

	hosts sync.Map
}

func (d *downloader) holdHost(ctx context.Context, rawURL string) (func(), error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" {
		return func() {}, nil
	}
	slot, _ := d.hosts.LoadOrStore(parsed.Host, make(chan struct{}, perHostLimit))
	gate := slot.(chan struct{})
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (d *downloader) run(ctx context.Context, jobs []job, phase string) error {
	total := int64(len(jobs))
	if phase != "" {
		progress.Report(ctx, progress.Event{Phase: phase, Total: total})
	}
	var done atomic.Int64
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(d.workers)
	for _, j := range jobs {
		j := j
		group.Go(func() error {
			if err := d.fetch(groupCtx, j); err != nil {
				return err
			}
			if phase != "" {
				progress.Report(ctx, progress.Event{Phase: phase, Current: done.Add(1), Total: total})
			}
			return nil
		})
	}
	return group.Wait()
}

func (d *downloader) fetch(ctx context.Context, j job) error {
	full, err := safepath.Join(d.root, j.path)
	if err != nil {
		return fmt.Errorf("метаданные просят положить файл мимо папки сборки: %w", err)
	}
	release, err := d.holdHost(ctx, j.url)
	if err != nil {
		return err
	}
	defer release()
	return downloadFile(ctx, d.http, j.url, full, j.digest, j.executable)
}

const (
	downloadAttempts = 5
	firstRetryPause  = time.Second
	longestPause     = 30 * time.Second
)

func downloadFile(ctx context.Context, client *http.Client, url, full string, want digest, executable bool) error {
	if want.sum != "" && alreadyThere(full, want) {
		return ensureMode(full, fileMode(executable))
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}

	pause := firstRetryPause
	for attempt := 1; ; attempt++ {
		err := fetchOnce(ctx, client, url, full, want, executable)
		if err == nil {
			return nil
		}
		var again retryable
		if !errors.As(err, &again) || attempt == downloadAttempts {
			return err
		}
		wait := pause
		if again.after > wait {
			wait = again.after
		}
		if wait > longestPause {
			wait = longestPause
		}
		slog.Warn("источник не отдал файл, повторяю", "url", url, "попытка", attempt, "через", wait.String(), "причина", err)
		if err := sleep(ctx, wait); err != nil {
			return err
		}
		pause *= 2
	}
}

func sleep(ctx context.Context, wait time.Duration) error {
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func alreadyThere(full string, want digest) bool {
	existing, err := os.Open(full)
	if err != nil {
		return false
	}
	defer existing.Close()
	hasher := want.hasher()
	if _, err := io.Copy(hasher, existing); err != nil {
		return false
	}
	return hex.EncodeToString(hasher.Sum(nil)) == want.sum
}

func ensureMode(path string, mode os.FileMode) error {
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() == mode.Perm() {
		return nil
	}
	return os.Chmod(path, mode)
}

func fileMode(executable bool) os.FileMode {
	if executable {
		return 0o755
	}
	return 0o644
}

type retryable struct {
	reason string
	cause  error
	after  time.Duration
}

func (r retryable) Error() string { return r.reason }

func (r retryable) Unwrap() error { return r.cause }

type sourceReader struct {
	from   io.Reader
	failed error
}

func (s *sourceReader) Read(p []byte) (int, error) {
	read, err := s.from.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		s.failed = err
	}
	return read, err
}

func retryAfter(resp *http.Response) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
	if err != nil || seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func worthRetrying(status int) bool {
	switch status {
	case http.StatusTooManyRequests, http.StatusRequestTimeout,
		http.StatusInternalServerError, http.StatusBadGateway,
		http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func fetchOnce(ctx context.Context, client *http.Client, url, full string, want digest, executable bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		return retryable{reason: fmt.Sprintf("не скачался %s: %v", url, err), cause: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		message := fmt.Sprintf("не скачался %s: сервер ответил %d", url, resp.StatusCode)
		if worthRetrying(resp.StatusCode) {
			return retryable{reason: message, after: retryAfter(resp)}
		}
		return errors.New(message)
	}

	tmp, err := os.CreateTemp(filepath.Dir(full), ".dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	source := &sourceReader{from: resp.Body}
	var reader io.Reader = source
	var hasher hash.Hash
	if want.sum != "" {
		hasher = want.hasher()
		reader = io.TeeReader(source, hasher)
	}
	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		if ctx.Err() != nil {
			return err
		}
		if source.failed != nil {
			return retryable{reason: fmt.Sprintf("оборвалась закачка %s: %v", url, err), cause: err}
		}
		return fmt.Errorf("не удалось записать %s: %w", full, err)
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if hasher != nil {
		if got := hex.EncodeToString(hasher.Sum(nil)); got != want.sum {
			return retryable{reason: fmt.Sprintf("файл %s скачался повреждённым: %s %s вместо %s", url, want.algorithm, got, want.sum)}
		}
	}
	if err := os.Chmod(tmp.Name(), fileMode(executable)); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), full)
}
