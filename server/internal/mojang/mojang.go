package mojang

import (
	"context"
	"net/http"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

const DefaultManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type Client struct {
	http        *http.Client
	manifestURL string
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 30 * time.Second}, manifestURL: DefaultManifestURL}
}

func NewClientWith(httpClient *http.Client, manifestURL string) *Client {
	return &Client{http: httpClient, manifestURL: manifestURL}
}

type VersionManifest struct {
	Latest struct {
		Release  string `json:"release"`
		Snapshot string `json:"snapshot"`
	} `json:"latest"`
	Versions []VersionSummary `json:"versions"`
}

type VersionSummary struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	URL         string    `json:"url"`
	ReleaseTime time.Time `json:"releaseTime"`
	SHA1        string    `json:"sha1"`
}

type JavaVersion struct {
	Component    string `json:"component"`
	MajorVersion int    `json:"majorVersion"`
}

type Download struct {
	SHA1 string `json:"sha1"`
	Size int64  `json:"size"`
	URL  string `json:"url"`
}

type AssetIndexRef struct {
	ID        string `json:"id"`
	SHA1      string `json:"sha1"`
	Size      int64  `json:"size"`
	TotalSize int64  `json:"totalSize"`
	URL       string `json:"url"`
}

type VersionDetail struct {
	ID          string        `json:"id"`
	Type        string        `json:"type"`
	MainClass   string        `json:"mainClass"`
	JavaVersion JavaVersion   `json:"javaVersion"`
	AssetIndex  AssetIndexRef `json:"assetIndex"`
	Libraries   []Library     `json:"libraries"`
	Downloads   struct {
		Client Download `json:"client"`
	} `json:"downloads"`
}

func (c *Client) ListVersions(ctx context.Context) (*VersionManifest, error) {
	var manifest VersionManifest
	if err := httpx.GetJSON(ctx, c.http, c.manifestURL, &manifest); err != nil {
		return nil, err
	}
	return &manifest, nil
}

func (c *Client) FetchVersion(ctx context.Context, url string) (*VersionDetail, error) {
	var detail VersionDetail
	if err := httpx.GetJSON(ctx, c.http, url, &detail); err != nil {
		return nil, err
	}
	return &detail, nil
}
