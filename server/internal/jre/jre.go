package jre

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

const DefaultAllRuntimesURL = "https://piston-meta.mojang.com/v1/products/java-runtime/2ec0cc96c44e5a76b9c8b7c39df7210883d12871/all.json"

type Client struct {
	http *http.Client
	url  string
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}, url: DefaultAllRuntimesURL}
}

func NewClientWith(httpClient *http.Client, url string) *Client {
	return &Client{http: httpClient, url: url}
}

type Download struct {
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type Runtime struct {
	Version struct {
		Name string `json:"name"`
	} `json:"version"`
	Manifest Download `json:"manifest"`
}

type allRuntimes map[string]map[string][]Runtime

var ErrNoMojangRuntime = errors.New("Mojang publishes no java runtime for this platform")

func (c *Client) Select(ctx context.Context, platformKey, component string) (*Runtime, error) {
	var all allRuntimes
	if err := httpx.GetJSON(ctx, c.http, c.url, &all); err != nil {
		return nil, err
	}
	platform, ok := all[platformKey]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNoMojangRuntime, platformKey)
	}
	runtimes := platform[component]
	if len(runtimes) == 0 {
		return nil, fmt.Errorf("no %q runtime for platform %q", component, platformKey)
	}
	runtime := runtimes[0]
	return &runtime, nil
}

type RuntimeFile struct {
	Path       string
	Download   Download
	Executable bool
}

type runtimeManifest struct {
	Files map[string]struct {
		Type       string `json:"type"`
		Executable bool   `json:"executable"`
		Downloads  struct {
			Raw Download `json:"raw"`
		} `json:"downloads"`
	} `json:"files"`
}

func (c *Client) FetchFiles(ctx context.Context, manifestURL string) ([]RuntimeFile, error) {
	var manifest runtimeManifest
	if err := httpx.GetJSON(ctx, c.http, manifestURL, &manifest); err != nil {
		return nil, err
	}
	files := make([]RuntimeFile, 0, len(manifest.Files))
	for path, entry := range manifest.Files {
		if entry.Type != "file" {
			continue
		}
		files = append(files, RuntimeFile{Path: path, Download: entry.Downloads.Raw, Executable: entry.Executable})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}
