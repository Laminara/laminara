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
	"strings"
	"time"
)

const (
	ChecksumsAsset = "checksums.txt"
	askTimeout     = 30 * time.Second
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

	assets map[string]string
}

func (r *Release) Asset(name string) (string, bool) {
	url, ok := r.assets[name]
	return url, ok
}

func (r *Release) Has(name string) bool {
	_, ok := r.assets[name]
	return ok
}

func (c *Client) Latest(ctx context.Context) (*Release, error) {
	return c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/latest", c.api(), c.Repo))
}

func (c *Client) ByTag(ctx context.Context, tag string) (*Release, error) {
	return c.get(ctx, fmt.Sprintf("%s/repos/%s/releases/tags/%s", c.api(), c.Repo, tag))
}

func (c *Client) get(ctx context.Context, url string) (*Release, error) {
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
		return nil, fmt.Errorf("у репозитория %s нет такого релиза", c.Repo)
	}
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub ответил %s", response.Status)
	}

	var payload struct {
		TagName     string    `json:"tag_name"`
		Body        string    `json:"body"`
		HTMLURL     string    `json:"html_url"`
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
		URL:         payload.HTMLURL,
		PublishedAt: payload.PublishedAt,
		assets:      make(map[string]string, len(payload.Assets)),
	}
	for _, asset := range payload.Assets {
		release.assets[asset.Name] = asset.URL
	}
	return release, nil
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

	file, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	digest := sha256.New()
	err = c.Fetch(ctx, url, io.MultiWriter(file, digest))
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(dest)
		return err
	}
	if got := hex.EncodeToString(digest.Sum(nil)); got != expected {
		os.Remove(dest)
		return fmt.Errorf("контрольная сумма %s не сошлась: скачано %s, в релизе %s", asset, got, expected)
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
	_, err = io.Copy(into, response.Body)
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
	return &http.Client{Timeout: askTimeout}
}

func (c *Client) downloadClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: askTimeout}).DialContext,
		TLSHandshakeTimeout:   askTimeout,
		ResponseHeaderTimeout: askTimeout,
	}}
}
