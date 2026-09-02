package selfupdate

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/ghrelease"
	"github.com/laminara/laminara/server/internal/version"
)

const (
	DefaultRepo     = "laminara/laminara"
	DefaultInterval = 24 * time.Hour
)

type Release struct {
	Version     string
	Tag         string
	Notes       string
	URL         string
	PublishedAt time.Time

	source *ghrelease.Release
}

type Checker struct {
	Repo   string
	Client *http.Client
	API    string
}

func (c *Checker) repo() string {
	if c.Repo == "" {
		return DefaultRepo
	}
	return c.Repo
}

func (c *Checker) releases() *ghrelease.Client {
	return &ghrelease.Client{Repo: c.repo(), API: c.API, HTTP: c.Client}
}

func AssetName() string {
	return fmt.Sprintf("laminara-server-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func (c *Checker) Latest(ctx context.Context) (*Release, error) {
	found, err := c.releases().Latest(ctx)
	if err != nil {
		return nil, err
	}
	if !found.Has(AssetName()) {
		return nil, fmt.Errorf("в релизе %s нет файла %s — эта система там не собрана", found.Tag, AssetName())
	}
	return &Release{
		Version:     found.Version,
		Tag:         found.Tag,
		Notes:       found.Notes,
		URL:         found.URL,
		PublishedAt: found.PublishedAt,
		source:      found,
	}, nil
}

func (r *Release) IsNewerThan(current string) bool {
	return version.IsNewer(r.Version, current)
}

func (c *Checker) Download(ctx context.Context, release *Release, dir string) (string, error) {
	staged := filepath.Join(dir, AssetName())
	if err := c.releases().Download(ctx, release.source, AssetName(), staged); err != nil {
		return "", err
	}
	if err := verifyVersion(staged, release.Version); err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func verifyVersion(binary, want string) error {
	output, err := exec.Command(binary, "version").Output()
	if err != nil {
		return fmt.Errorf("скачанный файл не запускается: %w", err)
	}
	got := strings.TrimSpace(string(output))
	if version.Compare(got, want) != 0 {
		return fmt.Errorf("скачанный файл представляется версией %s, а релиз обещал %s", got, want)
	}
	return nil
}

func Install(staged, target string) error {
	previous := target + ".old"
	os.Remove(previous)

	if err := os.Rename(target, previous); err != nil {
		return replaceError(target, err)
	}
	if err := os.Rename(staged, target); err != nil {
		os.Rename(previous, target)
		return replaceError(target, err)
	}
	return nil
}

func replaceError(target string, err error) error {
	return fmt.Errorf("не удалось заменить %s: %w", target, err)
}

func BinaryPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}
