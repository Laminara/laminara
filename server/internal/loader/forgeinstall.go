package loader

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/laminara/laminara/server/internal/forgeinstaller"
)

func runForgeInstaller(ctx context.Context, req InstallRequest, installerURL, cacheName string) (*InstallResult, error) {
	installerPath := filepath.Join(req.ProfileDir, ".laminara", cacheName)
	if err := req.Download(ctx, installerURL, installerPath, publishedSHA1(ctx, req, installerURL)); err != nil {
		return nil, err
	}

	installer, err := forgeinstaller.Open(installerPath)
	if err != nil {
		return nil, err
	}
	launch, err := installer.Install(ctx, forgeinstaller.Request{
		LibrariesDir: req.LibrariesDir,
		ProfileDir:   req.ProfileDir,
		MinecraftJar: req.MinecraftJar,
		JavaBin:      req.JavaBin,
		DataDir:      filepath.Join(req.ProfileDir, ".laminara", "installer-data"),
		Download:     forgeinstaller.Downloader(req.Download),
	})
	if err != nil {
		return nil, err
	}

	result := &InstallResult{
		MainClass: launch.MainClass,
		JVMArgs:   launch.JVMArgs,
		GameArgs:  launch.GameArgs,
		ClientJar: "libraries/" + launch.ClientJar,
	}
	if launch.ClientJar == "" {
		relative, err := filepath.Rel(req.ProfileDir, req.MinecraftJar)
		if err != nil {
			return nil, err
		}
		result.ClientJar = filepath.ToSlash(relative)
	}
	for _, library := range launch.Libraries {
		result.Libraries = append(result.Libraries, "libraries/"+library)
	}
	return result, nil
}

func publishedSHA1(ctx context.Context, req InstallRequest, artifactURL string) string {
	if req.Fetch == nil {
		return ""
	}
	body, err := req.Fetch(ctx, artifactURL+".sha1")
	if err != nil {
		return ""
	}
	digest := strings.Fields(strings.TrimSpace(string(body)))
	if len(digest) == 0 || len(digest[0]) != 40 {
		return ""
	}
	return strings.ToLower(digest[0])
}
