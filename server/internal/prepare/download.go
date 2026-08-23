package prepare

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"

	"golang.org/x/sync/errgroup"

	"github.com/laminara/laminara/server/internal/progress"
)

type job struct {
	url        string
	path       string
	sha1       string
	executable bool
}

type downloader struct {
	http    *http.Client
	root    string
	workers int
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
	full := filepath.Join(d.root, filepath.FromSlash(j.path))
	return downloadFile(ctx, d.http, j.url, full, j.sha1, j.executable)
}

func downloadFile(ctx context.Context, client *http.Client, url, full, sha1sum string, executable bool) error {
	if sha1sum != "" {
		if existing, err := os.Open(full); err == nil {
			hasher := sha1.New()
			_, copyErr := io.Copy(hasher, existing)
			existing.Close()
			if copyErr == nil && hex.EncodeToString(hasher.Sum(nil)) == sha1sum {
				return nil
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: status %d", url, resp.StatusCode)
	}

	tmp, err := os.CreateTemp(filepath.Dir(full), ".dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	var reader io.Reader = resp.Body
	var hasher hash.Hash
	if sha1sum != "" {
		hasher = sha1.New()
		reader = io.TeeReader(resp.Body, hasher)
	}
	if _, err := io.Copy(tmp, reader); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if hasher != nil {
		if got := hex.EncodeToString(hasher.Sum(nil)); got != sha1sum {
			return fmt.Errorf("download %s: sha1 mismatch (got %s, want %s)", url, got, sha1sum)
		}
	}
	mode := os.FileMode(0o644)
	if executable {
		mode = 0o755
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), full)
}
