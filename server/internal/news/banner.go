package news

import (
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	maxBannerBytes  = 512 << 10
	bannerCacheTTL  = 30 * time.Minute
	maxCachedBanner = 64
)

type bannerEntry struct {
	dataURI string
	fetched time.Time
}

type bannerCache struct {
	mu      sync.Mutex
	entries map[string]bannerEntry
	client  *http.Client
}

func newBannerCache() *bannerCache {
	return &bannerCache{entries: map[string]bannerEntry{}, client: &http.Client{Timeout: 10 * time.Second}}
}

func (c *bannerCache) inline(ctx context.Context, source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return ""
	}

	c.mu.Lock()
	entry, ok := c.entries[source]
	c.mu.Unlock()
	if ok && time.Since(entry.fetched) < bannerCacheTTL {
		return entry.dataURI
	}

	data, mime := c.read(ctx, source)
	dataURI := ""
	if len(data) > 0 {
		dataURI = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}

	c.mu.Lock()
	if len(c.entries) >= maxCachedBanner {
		c.entries = map[string]bannerEntry{}
	}
	c.entries[source] = bannerEntry{dataURI: dataURI, fetched: time.Now()}
	c.mu.Unlock()
	return dataURI
}

func (c *bannerCache) read(ctx context.Context, source string) ([]byte, string) {
	lower := strings.ToLower(source)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, ""
		}
		response, err := c.client.Do(request)
		if err != nil {
			return nil, ""
		}
		defer response.Body.Close()
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, ""
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxBannerBytes+1))
		if err != nil || len(data) > maxBannerBytes {
			return nil, ""
		}
		return data, imageMIME(source, response.Header.Get("Content-Type"))
	}

	info, err := os.Stat(source)
	if err != nil || info.Size() > maxBannerBytes {
		return nil, ""
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, ""
	}
	return data, imageMIME(source, "")
}

func imageMIME(source, declared string) string {
	switch strings.ToLower(filepath.Ext(strings.SplitN(source, "?", 2)[0])) {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	case ".svg":
		return "image/svg+xml"
	}
	for _, known := range []string{"image/png", "image/jpeg", "image/webp", "image/gif"} {
		if strings.HasPrefix(strings.ToLower(declared), known) {
			return known
		}
	}
	return "image/png"
}
