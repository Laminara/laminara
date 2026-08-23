package forgeinstaller

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestE2ENeoForgeInstall(t *testing.T) {
	installerJar := os.Getenv("LAMINARA_E2E_INSTALLER")
	clientJar := os.Getenv("LAMINARA_E2E_CLIENT")
	javaBin := os.Getenv("LAMINARA_E2E_JAVA")
	if installerJar == "" || clientJar == "" || javaBin == "" {
		t.Skip("E2E: set LAMINARA_E2E_INSTALLER, LAMINARA_E2E_CLIENT, LAMINARA_E2E_JAVA")
	}

	installer, err := Open(installerJar)
	if err != nil {
		t.Fatal(err)
	}

	work := t.TempDir()
	request := Request{
		LibrariesDir: filepath.Join(work, "libraries"),
		ProfileDir:   filepath.Join(work, "profile"),
		DataDir:      filepath.Join(work, "data"),
		MinecraftJar: clientJar,
		JavaBin:      javaBin,
		Download:     httpDownload,
	}
	if err := os.MkdirAll(request.ProfileDir, 0o755); err != nil {
		t.Fatal(err)
	}

	launch, err := installer.Install(context.Background(), request)
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	t.Logf("mainClass=%s jvmArgs=%d gameArgs=%d libraries=%d", launch.MainClass, len(launch.JVMArgs), len(launch.GameArgs), len(launch.Libraries))

	placeholders, err := installer.placeholders(request)
	if err != nil {
		t.Fatal(err)
	}
	patched := placeholders["PATCHED"]
	info, err := os.Stat(patched)
	if err != nil {
		t.Fatalf("patched jar missing: %v", err)
	}
	t.Logf("patched jar: %s (%d bytes)", patched, info.Size())

	reader, err := zip.OpenReader(patched)
	if err != nil {
		t.Fatalf("patched jar not a valid zip: %v", err)
	}
	defer reader.Close()
	var minecraftClasses int
	for _, file := range reader.File {
		if strings.HasPrefix(file.Name, "net/minecraft/") {
			minecraftClasses++
		}
	}
	if minecraftClasses == 0 {
		t.Fatal("patched jar contains no net/minecraft classes")
	}
	t.Logf("patched jar contains %d net/minecraft entries", minecraftClasses)
}

func httpDownload(ctx context.Context, url, dest, _ string) error {
	if _, err := os.Stat(dest); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: status %d", url, response.StatusCode)
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, response.Body)
	return err
}
