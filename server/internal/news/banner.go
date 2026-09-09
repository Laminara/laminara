package news

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/laminara/laminara/server/internal/mediatype"
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
	ours    bool
}

func newBannerCache(ours bool) *bannerCache {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			addr, err := netip.ParseAddr(host)
			if err != nil {
				return err
			}
			if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsUnspecified() {
				return errors.New("баннер новостей ведёт на служебный адрес машины — такие ссылки сервер не открывает")
			}
			return nil
		},
	}
	return &bannerCache{
		entries: map[string]bannerEntry{},
		ours:    ours,
		client: &http.Client{
			Timeout:   10 * time.Second,
			Transport: &http.Transport{DialContext: dialer.DialContext},
		},
	}
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

	fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	data, mime := c.read(fetchCtx, source)
	cancel()
	dataURI := ""
	if len(data) > 0 {
		dataURI = "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
	}

	c.mu.Lock()
	if len(c.entries) >= maxCachedBanner {
		c.forgetOldestLocked()
	}
	c.entries[source] = bannerEntry{dataURI: dataURI, fetched: time.Now()}
	c.mu.Unlock()
	return dataURI
}

func (c *bannerCache) forgetOldestLocked() {
	oldestKey := ""
	oldest := time.Time{}
	for key, entry := range c.entries {
		if oldestKey == "" || entry.fetched.Before(oldest) {
			oldestKey, oldest = key, entry.fetched
		}
	}
	if oldestKey != "" {
		delete(c.entries, oldestKey)
	}
}

func (c *bannerCache) read(ctx context.Context, source string) ([]byte, string) {
	if isWebLink(source) {
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
		return data, mediatype.GuessImage(source, response.Header.Get("Content-Type"))
	}

	if !c.ours {
		slog.Default().Warn("баннер новостей просит файл с сервера — отдаю только картинки по ссылке",
			"source", "news",
			"баннер", source,
		)
		return nil, ""
	}
	info, err := os.Stat(source)
	if err != nil || info.Size() > maxBannerBytes {
		return nil, ""
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, ""
	}
	return data, mediatype.GuessImage(source, "")
}
