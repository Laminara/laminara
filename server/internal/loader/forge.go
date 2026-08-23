package loader

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/laminara/laminara/server/internal/httpx"
)

func init() {
	Register(newForge())
}

const forgeMetadataURL = "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"

type forge struct {
	http        *http.Client
	metadataURL string
}

func newForge() *forge {
	return &forge{http: &http.Client{Timeout: 30 * time.Second}, metadataURL: forgeMetadataURL}
}

func (f *forge) Name() string { return "forge" }

func (f *forge) Versions(ctx context.Context, mcVersion string) ([]string, error) {
	var metadata struct {
		Versions []string `xml:"versioning>versions>version"`
	}
	if err := httpx.GetXML(ctx, f.http, f.metadataURL, &metadata); err != nil {
		return nil, err
	}
	prefix := mcVersion + "-"
	var matched []string
	for _, version := range metadata.Versions {
		if suffix, ok := strings.CutPrefix(version, prefix); ok {
			matched = append(matched, suffix)
		}
	}
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}
	return matched, nil
}

func (f *forge) Resolve(_ context.Context, _, _ string) (*LoaderProfile, error) {
	return nil, fmt.Errorf("forge is a transformative loader; use the installer path")
}

func (f *forge) Install(ctx context.Context, req InstallRequest) (*InstallResult, error) {
	fullVersion := req.MCVersion + "-" + req.LoaderVersion
	installerURL := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", fullVersion, fullVersion)
	return runForgeInstaller(ctx, req, installerURL, fmt.Sprintf("forge-%s-installer.jar", fullVersion))
}
