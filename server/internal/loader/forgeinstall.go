package loader

import (
	"context"
	"path/filepath"

	"github.com/laminara/laminara/server/internal/forgeinstaller"
)

func runForgeInstaller(ctx context.Context, req InstallRequest, installerURL, cacheName string) (*InstallResult, error) {
	installerPath := filepath.Join(req.ProfileDir, ".laminara", cacheName)
	if err := req.Download(ctx, installerURL, installerPath, ""); err != nil {
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
