package compat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/ghrelease"
	"github.com/laminara/laminara/server/internal/httpx"
	"github.com/laminara/laminara/server/internal/mojang"
)

func init() {
	Register(newLwjgl3ify())
}

const (
	lwjgl3ifyRepo   = "GTNewHorizons/lwjgl3ify"
	unimixinsRepo   = "LegacyModdingMC/UniMixins"
	lwjgl3ifyTarget = "1.7.10"
	manifestAsset   = "version.json"
)

var forgeInVersionID = regexp.MustCompile(`Forge([0-9]+(?:\.[0-9]+)*)`)

type lwjgl3ify struct {
	releases  *ghrelease.Client
	unimixins *ghrelease.Client
	http      *http.Client
}

func newLwjgl3ify() *lwjgl3ify {
	return newLwjgl3ifyWith("", httpx.NewClient(30*time.Second))
}

func newLwjgl3ifyWith(api string, client *http.Client) *lwjgl3ify {
	return &lwjgl3ify{
		releases:  &ghrelease.Client{Repo: lwjgl3ifyRepo, API: api, HTTP: client},
		unimixins: &ghrelease.Client{Repo: unimixinsRepo, API: api, HTTP: client},
		http:      client,
	}
}

func (l *lwjgl3ify) Name() string { return "lwjgl3ify" }

func (l *lwjgl3ify) Summary() string {
	return "Minecraft 1.7.10 на LWJGL 3 и современной Java (GTNewHorizons)"
}

func (l *lwjgl3ify) Loader() string { return "forge" }

func (l *lwjgl3ify) Supports(mcVersion string) bool { return mcVersion == lwjgl3ifyTarget }

func (l *lwjgl3ify) Versions(ctx context.Context) ([]string, error) {
	releases, err := l.releases.List(ctx, 30)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		if release.Has(manifestAsset) {
			versions = append(versions, release.Version)
		}
	}
	return versions, nil
}

func (l *lwjgl3ify) Resolve(ctx context.Context, mcVersion, version string) (*Plan, error) {
	release, err := l.release(ctx, version)
	if err != nil {
		return nil, err
	}
	manifestURL, ok := release.Asset(manifestAsset)
	if !ok {
		return nil, fmt.Errorf("в релизе lwjgl3ify %s нет %s — возьмите другую версию рецепта", release.Tag, manifestAsset)
	}

	detail, err := l.manifest(ctx, manifestURL)
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(detail.ID, mcVersion) {
		return nil, fmt.Errorf("рецепт lwjgl3ify %s собран под другую версию Minecraft: %s", release.Version, detail.ID)
	}

	modName := fmt.Sprintf("lwjgl3ify-%s.jar", release.Version)
	modURL, ok := release.Asset(modName)
	if !ok {
		return nil, fmt.Errorf("в релизе lwjgl3ify %s нет мода %s", release.Tag, modName)
	}

	plan := &Plan{
		Recipe:        l.Name(),
		Version:       release.Version,
		VersionURL:    manifestURL,
		VersionID:     detail.ID,
		LoaderName:    "forge",
		LoaderVersion: forgeVersionOf(detail.ID),
		JavaMajor:     detail.JavaVersion.MajorVersion,
		Prerelease:    release.Prerelease,
		Files: []File{{
			Path:   "mods/" + modName,
			URL:    modURL,
			SHA256: release.SHA256(modName),
		}},
	}

	mixins, err := l.unimixinsFile(ctx)
	if err != nil {
		return nil, err
	}
	plan.Files = append(plan.Files, mixins)
	plan.Notes = append(plan.Notes,
		"Hodgepodge (GTNewHorizons/Hodgepodge) не ставится сам — он лечит совместимость отдельных модов, положите его в mods сами, если он нужен.",
	)
	return plan, nil
}

func (l *lwjgl3ify) release(ctx context.Context, version string) (*ghrelease.Release, error) {
	if version == "" {
		return l.releases.Latest(ctx)
	}
	return l.releases.ByTag(ctx, version)
}

func (l *lwjgl3ify) manifest(ctx context.Context, url string) (*mojang.VersionDetail, error) {
	var body strings.Builder
	if err := l.releases.Fetch(ctx, url, &body); err != nil {
		return nil, fmt.Errorf("манифест рецепта lwjgl3ify не скачался: %w", err)
	}
	var detail mojang.VersionDetail
	if err := json.Unmarshal([]byte(body.String()), &detail); err != nil {
		return nil, fmt.Errorf("манифест рецепта lwjgl3ify не разобрать: %w", err)
	}
	return &detail, nil
}

func (l *lwjgl3ify) unimixinsFile(ctx context.Context) (File, error) {
	release, err := l.unimixins.Latest(ctx)
	if err != nil {
		return File{}, fmt.Errorf("lwjgl3ify без UniMixins не запустится, а его релиз не читается: %w", err)
	}
	name := "+unimixins-all-" + lwjgl3ifyTarget + "-" + release.Version + ".jar"
	url, ok := release.Asset(name)
	if !ok {
		return File{}, fmt.Errorf("в релизе UniMixins %s нет %s — без него lwjgl3ify не запустится", release.Tag, name)
	}
	return File{Path: "mods/" + name, URL: url, SHA256: release.SHA256(name)}, nil
}

func forgeVersionOf(versionID string) string {
	match := forgeInVersionID.FindStringSubmatch(versionID)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}
