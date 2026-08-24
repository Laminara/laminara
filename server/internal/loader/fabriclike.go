package loader

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

const fabricMaven = "https://maven.fabricmc.net/"

func init() {
	Register(newFabricLike("fabric", "https://meta.fabricmc.net/v2", fabricMaven))
	Register(newFabricLike("quilt", "https://meta.quiltmc.org/v3", "https://maven.quiltmc.org/repository/release/"))
}

type fabricLike struct {
	name     string
	http     *http.Client
	baseURL  string
	mavenURL string
}

func newFabricLike(name, baseURL, mavenURL string) *fabricLike {
	return &fabricLike{name: name, http: &http.Client{Timeout: 30 * time.Second}, baseURL: baseURL, mavenURL: mavenURL}
}

func (f *fabricLike) Name() string { return f.name }

type fabricLib struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

type fabricEntry struct {
	Loader struct {
		Version string `json:"version"`
		Maven   string `json:"maven"`
	} `json:"loader"`
	Intermediary struct {
		Maven string `json:"maven"`
	} `json:"intermediary"`
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
	for _, coordinate := range []string{entry.Intermediary.Maven, entry.Loader.Maven} {
		if coordinate == "" {
			continue
		}
		profile.Libraries = append(profile.Libraries, Library{Name: coordinate, URL: f.mavenOf(coordinate)})
	}
	for _, lib := range append(entry.LauncherMeta.Libraries.Common, entry.LauncherMeta.Libraries.Client...) {
		profile.Libraries = append(profile.Libraries, Library{Name: lib.Name, URL: lib.URL})
	}
	return profile, nil
}

func (f *fabricLike) mavenOf(coordinate string) string {
	if strings.HasPrefix(coordinate, "net.fabricmc:") {
		return fabricMaven
	}
	return f.mavenURL
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
