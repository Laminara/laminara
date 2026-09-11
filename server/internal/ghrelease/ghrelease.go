package ghrelease

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

const (
	ChecksumsAsset = "checksums.txt"
	askTimeout     = 30 * time.Second
	maxAsset       = 512 << 20
)

type Client struct {
	Repo string
	API  string
	HTTP *http.Client
}

type Release struct {
	Version     string
	Tag         string
	Notes       string
	URL         string
	PublishedAt time.Time
	Prerelease  bool

	assets  map[string]string
	digests map[string]string
}

func (r *Release) Asset(name string) (string, bool) {
	url, ok := r.assets[name]
	return url, ok
}

func (r *Release) Has(name string) bool {
	_, ok := r.assets[name]
	return ok
}

func (r *Release) AssetNames() []string {
	names := make([]string, 0, len(r.assets))
	for name := range r.assets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (r *Release) SHA256(name string) string {
	return r.digests[name]
}

func (c *Client) Latest(ctx context.Context) (*Release, error) {
	return c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", c.api(), c.Repo))
}

func (c *Client) ByTag(ctx context.Context, tag string) (*Release, error) {
	return c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.api(), c.Repo, tag))
}

type releasePayload struct {
	TagName     string    `json:"tag_name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Prerelease  bool      `json:"prerelease"`
	Draft       bool      `json:"draft"`
	Assets      []struct {
		Name   string `json:"name"`
		URL    string `json:"browser_download_url"`
		Digest string `json:"digest"`
	} `json:"assets"`
}

func (p releasePayload) release() *Release {
	release := &Release{
		Version:     strings.TrimPrefix(p.TagName, "v"),
		Tag:         p.TagName,
		Notes:       strings.TrimSpace(p.Body),
		URL:         p.HTMLURL,
		PublishedAt: p.PublishedAt,
		Prerelease:  p.Prerelease,
		assets:      make(map[string]string, len(p.Assets)),
		digests:     make(map[string]string, len(p.Assets)),
	}
	for _, asset := range p.Assets {
		release.assets[asset.Name] = asset.URL
		if digest, ok := strings.CutPrefix(asset.Digest, "sha256:"); ok {
			release.digests[asset.Name] = strings.ToLower(digest)
		}
	}
	return release
}

func (c *Client) List(ctx context.Context, limit int) ([]*Release, error) {
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	body, err := c.ask(ctx, fmt.Sprintf("%s/repos/%s/releases?per_page=%d", c.api(), c.Repo, limit))
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var payloads []releasePayload
	if err := json.NewDecoder(body).Decode(&payloads); err != nil {
		return nil, err
	}
	releases := make([]*Release, 0, len(payloads))
	for _, payload := range payloads {
		if payload.Draft {
			continue
		}
		releases = append(releases, payload.release())
	}
	return releases, nil
}

func (c *Client) get(ctx context.Context, url string) (*Release, error) {
	body, err := c.ask(ctx, url)
	if err != nil {
		return nil, err
	}
	defer body.Close()

	var payload releasePayload
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.release(), nil
}

func (c *Client) ask(ctx context.Context, url string) (io.ReadCloser, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("LAMINARA_GITHUB_TOKEN"); token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := c.client().Do(request)
	if err != nil {
		return nil, fmt.Errorf("не удалось спросить GitHub про релизы: %w", err)
	}
	if response.StatusCode == http.StatusNotFound {
		response.Body.Close()
		return nil, fmt.Errorf("у репозитория %s нет такого релиза", c.Repo)
	}
	if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
		response.Body.Close()
		return nil, fmt.Errorf("GitHub временно не отвечает на запросы про %s — подождите час или задайте токен в LAMINARA_GITHUB_TOKEN", c.Repo)
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("GitHub ответил %s", response.Status)
	}
	return response.Body, nil
}

func (c *Client) Download(ctx context.Context, release *Release, asset, dest string) error {
	url, ok := release.Asset(asset)
	if !ok {
		return fmt.Errorf("в релизе %s нет файла %s", release.Tag, asset)
	}
	expected, err := c.checksum(ctx, release, asset)
	if err != nil {
		return err
	}

	partial := dest + ".part"
	file, err := os.OpenFile(partial, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	digest := sha256.New()
	err = c.Fetch(ctx, url, io.MultiWriter(file, digest))
	if syncErr := file.Sync(); err == nil {
		err = syncErr
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(partial)
		return err
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != expected {
		os.Remove(partial)
		return fmt.Errorf("контрольная сумма %s не сошлась: скачано %s, в релизе %s", asset, got, expected)
	}
	if err := os.Rename(partial, dest); err != nil {
		os.Remove(partial)
		return err
	}
	return nil
}

func (c *Client) checksum(ctx context.Context, release *Release, asset string) (string, error) {
	url, ok := release.Asset(ChecksumsAsset)
	if !ok {
		return "", fmt.Errorf("в релизе %s нет файла %s — без него скачанное не проверить", release.Tag, ChecksumsAsset)
	}
	var body strings.Builder
	if err := c.Fetch(ctx, url, &body); err != nil {
		return "", err
	}
	for _, line := range strings.Split(body.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", fmt.Errorf("в %s нет строки про %s", ChecksumsAsset, asset)
}

func (c *Client) Fetch(ctx context.Context, url string, into io.Writer) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := c.downloadClient().Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("%s отдал %s", url, response.Status)
	}
	_, err = io.Copy(into, io.LimitReader(response.Body, maxAsset))
	return err
}

func (c *Client) api() string {
	if c.API == "" {
		return "https://api.github.com"
	}
	return strings.TrimSuffix(c.API, "/")
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return httpx.NewClient(askTimeout)
}

func (c *Client) downloadClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Transport: httpx.WithUserAgent(&http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: askTimeout}).DialContext,
		TLSHandshakeTimeout:   askTimeout,
		ResponseHeaderTimeout: askTimeout,
	})}
}
