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
	Register(newNeoForge())
}

const neoForgeVersionsURL = "https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge"

type neoForge struct {
	http        *http.Client
	versionsURL string
}

func newNeoForge() *neoForge {
	return &neoForge{http: &http.Client{Timeout: 30 * time.Second}, versionsURL: neoForgeVersionsURL}
}

func (n *neoForge) Name() string { return "neoforge" }

func (n *neoForge) Versions(ctx context.Context, mcVersion string) ([]string, error) {
	prefix, ok := neoForgePrefix(mcVersion)
	if !ok {
		return nil, nil
	}
	var document struct {
		Versions []string `json:"versions"`
	}
	if err := httpx.GetJSON(ctx, n.http, n.versionsURL, &document); err != nil {
		return nil, err
	}
	var matched []string
	for _, version := range document.Versions {
		if strings.HasPrefix(version, prefix) {
			matched = append(matched, version)
		}
	}
	for i, j := 0, len(matched)-1; i < j; i, j = i+1, j-1 {
		matched[i], matched[j] = matched[j], matched[i]
	}
	return matched, nil
}

func (n *neoForge) Resolve(_ context.Context, _, _ string) (*LoaderProfile, error) {
	return nil, fmt.Errorf("neoforge is a transformative loader; use the installer path")
}

func (n *neoForge) Install(ctx context.Context, req InstallRequest) (*InstallResult, error) {
	installerURL := fmt.Sprintf("https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar", req.LoaderVersion, req.LoaderVersion)
	return runForgeInstaller(ctx, req, installerURL, fmt.Sprintf("neoforge-%s-installer.jar", req.LoaderVersion))
}

func neoForgePrefix(mcVersion string) (string, bool) {
	parts := strings.Split(mcVersion, ".")
	if len(parts) < 2 || parts[0] != "1" {
		return "", false
	}
	minor := "0"
	if len(parts) >= 3 {
		minor = parts[2]
	}
	return parts[1] + "." + minor + ".", true
}
