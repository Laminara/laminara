package loader

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

func init() {
	Register(newFabricLike("fabric", "https://meta.fabricmc.net/v2"))
	Register(newFabricLike("quilt", "https://meta.quiltmc.org/v3"))
}

type fabricLike struct {
	name    string
	http    *http.Client
	baseURL string
}

func newFabricLike(name, baseURL string) *fabricLike {
	return &fabricLike{name: name, http: &http.Client{Timeout: 30 * time.Second}, baseURL: baseURL}
}

func (f *fabricLike) Name() string { return f.name }

type fabricLib struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type fabricEntry struct {
	Loader struct {
		Version string `json:"version"`
	} `json:"loader"`
	LauncherMeta struct {
		MainClass json.RawMessage `json:"mainClass"`
		Libraries struct {
			Common []fabricLib `json:"common"`
			Client []fabricLib `json:"client"`
		} `json:"libraries"`
	} `json:"launcherMeta"`
}

func (f *fabricLike) Versions(ctx context.Context, mcVersion string) ([]string, error) {
	var entries []fabricEntry
	if err := httpx.GetJSON(ctx, f.http, f.baseURL+"/versions/loader/"+mcVersion, &entries); err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		versions = append(versions, entry.Loader.Version)
	}
	return versions, nil
}

func (f *fabricLike) Resolve(ctx context.Context, mcVersion, loaderVersion string) (*LoaderProfile, error) {
	var entry fabricEntry
	if err := httpx.GetJSON(ctx, f.http, f.baseURL+"/versions/loader/"+mcVersion+"/"+loaderVersion, &entry); err != nil {
		return nil, err
	}
	mainClass, err := parseMainClass(entry.LauncherMeta.MainClass)
	if err != nil {
		return nil, err
	}
	profile := &LoaderProfile{MainClass: mainClass}
	for _, lib := range append(entry.LauncherMeta.Libraries.Common, entry.LauncherMeta.Libraries.Client...) {
		profile.Libraries = append(profile.Libraries, Library{Name: lib.Name, URL: lib.URL})
	}
	return profile, nil
}

func parseMainClass(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString, nil
	}
	var asObject struct {
		Client string `json:"client"`
	}
	if err := json.Unmarshal(raw, &asObject); err != nil {
		return "", err
	}
	return asObject.Client, nil
}
