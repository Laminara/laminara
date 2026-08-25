package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/version"
)

const (
	DefaultRepo     = "MrLeonardosMi/Laminara"
	DefaultInterval = 24 * time.Hour
	checksumsAsset  = "checksums.txt"
)

type Release struct {
	Version     string
	Tag         string
	Notes       string
	PublishedAt time.Time
	assetName   string
	assetURL    string
	checksumURL string
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

func (c *Checker) client() *http.Client {
	if c.Client != nil {
		return c.Client
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Checker) api() string {
	if c.API == "" {
		return "https://api.github.com"
	}
	return strings.TrimSuffix(c.API, "/")
}

func AssetName() string {
	return fmt.Sprintf("laminara-server-%s-%s", runtime.GOOS, runtime.GOARCH)
}

func (c *Checker) Latest(ctx context.Context) (*Release, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", c.api(), c.repo())
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")

	response, err := c.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("не удалось спросить GitHub про релизы: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("у репозитория %s нет ни одного релиза", c.repo())
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub ответил %s", response.Status)
	}

	var payload struct {
		TagName     string    `json:"tag_name"`
		Body        string    `json:"body"`
		Draft       bool      `json:"draft"`
		Prerelease  bool      `json:"prerelease"`
		PublishedAt time.Time `json:"published_at"`
		Assets      []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}

	release := &Release{
		Version:     strings.TrimPrefix(payload.TagName, "v"),
		Tag:         payload.TagName,
		Notes:       strings.TrimSpace(payload.Body),
		PublishedAt: payload.PublishedAt,
		assetName:   AssetName(),
	}
	for _, asset := range payload.Assets {
		switch asset.Name {
		case release.assetName:
			release.assetURL = asset.URL
		case checksumsAsset:
			release.checksumURL = asset.URL
		}
	}
	if release.assetURL == "" {
		return nil, fmt.Errorf("в релизе %s нет файла %s — эта система там не собрана", release.Tag, release.assetName)
	}
	return release, nil
}

func (r *Release) IsNewerThan(current string) bool {
	return version.IsNewer(r.Version, current)
}

func (c *Checker) Download(ctx context.Context, release *Release, dir string) (string, error) {
	expected, err := c.checksum(ctx, release)
	if err != nil {
		return "", err
	}

	staged := filepath.Join(dir, release.assetName)
	file, err := os.OpenFile(staged, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return "", err
	}

	digest := sha256.New()
	err = c.fetch(ctx, release.assetURL, io.MultiWriter(file, digest))
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(staged)
		return "", err
	}

	if got := hex.EncodeToString(digest.Sum(nil)); got != expected {
		os.Remove(staged)
		return "", fmt.Errorf("контрольная сумма не сошлась: скачано %s, в релизе %s", got, expected)
	}

	if err := verifyVersion(staged, release.Version); err != nil {
		os.Remove(staged)
		return "", err
	}
	return staged, nil
}

func (c *Checker) checksum(ctx context.Context, release *Release) (string, error) {
	if release.checksumURL == "" {
		return "", fmt.Errorf("в релизе %s нет файла %s — без него обновление не проверить", release.Tag, checksumsAsset)
	}
	var body strings.Builder
	if err := c.fetch(ctx, release.checksumURL, &body); err != nil {
		return "", err
	}
	for _, line := range strings.Split(body.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == release.assetName {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("в %s нет строки про %s", checksumsAsset, release.assetName)
}

func (c *Checker) fetch(ctx context.Context, url string, into io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := c.client().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s отдал %s", url, response.Status)
	}
	_, err = io.Copy(into, response.Body)
	return err
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
		if restore := os.Rename(previous, target); restore != nil {
			return fmt.Errorf("%s не заменён и не восстановлен, старая версия лежит в %s: %w", target, previous, err)
		}
		return replaceError(target, err)
	}
	return nil
}

func replaceError(target string, err error) error {
	if errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("нет прав заменить %s — запустите обновление от пользователя, которому принадлежит файл: %w", target, err)
	}
	return err
}

func BinaryPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}
