package loader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/laminara/laminara/server/internal/forgeinstaller"
)

func runForgeInstaller(ctx context.Context, req InstallRequest, installerURL, cacheName string) (*InstallResult, error) {
	installerPath := filepath.Join(req.ProfileDir, ".laminara", cacheName)
	if err := req.Download(ctx, installerURL, installerPath, publishedSHA1(ctx, req, installerURL)); err != nil {
		return nil, err
	}
	return installFromJar(ctx, req, installerPath)
}

func runVerifiedInstaller(ctx context.Context, req InstallRequest, installerURL, cacheName, sha256sum string) (*InstallResult, error) {
	if sha256sum == "" {
		return nil, fmt.Errorf("для установщика %s нет контрольной суммы — без неё скачанное не проверить", cacheName)
	}
	installerPath := filepath.Join(req.ProfileDir, ".laminara", cacheName)
	if err := req.Download(ctx, installerURL, installerPath, ""); err != nil {
		return nil, err
	}
	if err := checkSHA256(installerPath, sha256sum); err != nil {
		os.Remove(installerPath)
		return nil, err
	}
	return installFromJar(ctx, req, installerPath)
}

func installFromJar(ctx context.Context, req InstallRequest, installerPath string) (*InstallResult, error) {
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
	for _, library := range launch.OnDisk {
		result.OnDisk = append(result.OnDisk, "libraries/"+library)
	}
	return result, nil
}

func checkSHA256(path, want string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return err
	}
	if got := hex.EncodeToString(hasher.Sum(nil)); got != strings.ToLower(want) {
		return fmt.Errorf("установщик %s скачался повреждённым: sha256 %s вместо %s", filepath.Base(path), got, want)
	}
	return nil
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
