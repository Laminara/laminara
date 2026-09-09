package loader

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
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
	sort.SliceStable(matched, func(i, j int) bool {
		return newerVersion(matched[i], matched[j])
	})
	return matched, nil
}

func (f *forge) Resolve(_ context.Context, _, _ string) (*LoaderProfile, error) {
	return nil, fmt.Errorf("forge ставится через свой установщик, а не как обычный загрузчик")
}

func (f *forge) Install(ctx context.Context, req InstallRequest) (*InstallResult, error) {
	fullVersion := req.MCVersion + "-" + req.LoaderVersion
	installerURL := fmt.Sprintf("https://maven.minecraftforge.net/net/minecraftforge/forge/%s/forge-%s-installer.jar", fullVersion, fullVersion)
	return runForgeInstaller(ctx, req, installerURL, fmt.Sprintf("forge-%s-installer.jar", fullVersion))
}

func newerVersion(left, right string) bool {
	first, second := numericParts(left), numericParts(right)
	for i := 0; i < len(first) && i < len(second); i++ {
		if first[i] != second[i] {
			return first[i] > second[i]
		}
	}
	if len(first) != len(second) {
		return len(first) > len(second)
	}
	return left > right
}

func numericParts(version string) []int {
	head, _, _ := strings.Cut(version, "-")
	fields := strings.Split(head, ".")
	parts := make([]int, 0, len(fields))
	for _, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			break
		}
		parts = append(parts, value)
	}
	return parts
}
