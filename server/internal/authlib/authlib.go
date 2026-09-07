package authlib

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/laminara/laminara/server/internal/httpx"
)

const (
	FileName = "authlib-injector.jar"
	feedURL  = "https://authlib-injector.yushi.moe/artifact/latest.json"
)

type release struct {
	Version     string `json:"version"`
	DownloadURL string `json:"download_url"`
	Checksums   struct {
		SHA256 string `json:"sha256"`
	} `json:"checksums"`
}

func Present(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil && info.Size() > 0
}

func Ensure(ctx context.Context, client *http.Client, dir string, fetch func(ctx context.Context, url, dest, sha1 string) error) (string, error) {
	if Present(dir) {
		return "", nil
	}
	latest, err := latestRelease(ctx, client)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, FileName)
	if err := fetch(ctx, latest.DownloadURL, target, ""); err != nil {
		return "", err
	}
	if err := verify(target, latest.Checksums.SHA256); err != nil {
		_ = os.Remove(target)
		return "", err
	}
	return latest.Version, nil
}

func latestRelease(ctx context.Context, client *http.Client) (*release, error) {
	var latest release
	if err := getJSON(ctx, client, feedURL, &latest); err != nil {
		return nil, err
	}
	if latest.DownloadURL == "" {
		return nil, fmt.Errorf("источник authlib-injector не назвал ссылку на файл")
	}
	return &latest, nil
}

func verify(path, want string) error {
	if want == "" {
		return nil
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return err
	}
	got := hex.EncodeToString(sum.Sum(nil))
	if got != want {
		return fmt.Errorf("скачанный authlib-injector не совпал с контрольной суммой источника")
	}
	return nil
}

func getJSON(ctx context.Context, client *http.Client, url string, into any) error {
	return httpx.GetJSON(ctx, client, url, into)
}
