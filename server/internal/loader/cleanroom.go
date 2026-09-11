package loader

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/ghrelease"
	"github.com/laminara/laminara/server/internal/httpx"
)

func init() {
	Register(newCleanroom())
}

const (
	cleanroomRepo    = "CleanroomMC/Cleanroom"
	cleanroomTarget  = "1.12.2"
	cleanroomJavaKey = "java-runtime-epsilon"
)

type cleanroom struct {
	releases *ghrelease.Client
}

func newCleanroom() *cleanroom {
	return &cleanroom{releases: &ghrelease.Client{Repo: cleanroomRepo, HTTP: httpx.NewClient(30 * time.Second)}}
}

func (c *cleanroom) Name() string { return "cleanroom" }

func (c *cleanroom) JavaComponent() string { return cleanroomJavaKey }

func (c *cleanroom) Versions(ctx context.Context, mcVersion string) ([]string, error) {
	if mcVersion != cleanroomTarget {
		return nil, nil
	}
	releases, err := c.releases.List(ctx, 30)
	if err != nil {
		return nil, err
	}
	versions := make([]string, 0, len(releases))
	for _, release := range releases {
		if release.Has(cleanroomInstaller(release.Version)) {
			versions = append(versions, release.Version)
		}
	}
	return versions, nil
}

func (c *cleanroom) Resolve(_ context.Context, _, _ string) (*LoaderProfile, error) {
	return nil, fmt.Errorf("cleanroom ставится через свой установщик, а не как обычный загрузчик")
}

func (c *cleanroom) Install(ctx context.Context, req InstallRequest) (*InstallResult, error) {
	if req.MCVersion != cleanroomTarget {
		return nil, fmt.Errorf("cleanroom бывает только под Minecraft %s", cleanroomTarget)
	}
	release, err := c.releases.ByTag(ctx, req.LoaderVersion)
	if err != nil {
		return nil, err
	}
	asset := cleanroomInstaller(release.Version)
	installerURL, ok := release.Asset(asset)
	if !ok {
		return nil, fmt.Errorf("в релизе cleanroom %s нет установщика %s", release.Tag, asset)
	}
	return runVerifiedInstaller(ctx, req, installerURL, asset, release.SHA256(asset))
}

func cleanroomInstaller(version string) string {
	return "cleanroom-" + version + "-installer.jar"
}

func Prerelease(version string) bool {
	lowered := strings.ToLower(version)
	for _, marker := range []string{"alpha", "beta", "-rc", "snapshot", "-pre"} {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}
