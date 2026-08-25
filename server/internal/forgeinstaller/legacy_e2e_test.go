package forgeinstaller

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLegacyInstallerAgainstRealForge(t *testing.T) {
	if os.Getenv("LAMINARA_E2E_LEGACY") == "" {
		t.Skip("E2E: set LAMINARA_E2E_LEGACY=1 to download real Forge installers")
	}

	cases := []struct {
		version   string
		mcVersion string
		mainClass string
		legacy    bool
	}{
		{version: "1.7.10-10.13.4.1614-1.7.10", mcVersion: "1.7.10", mainClass: "net.minecraft.launchwrapper.Launch", legacy: true},
		{version: "1.12.2-14.23.5.2860", mcVersion: "1.12.2", mainClass: "net.minecraft.launchwrapper.Launch"},
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	download := func(ctx context.Context, url, dest, _ string) error {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		response, err := client.Do(request)
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return os.ErrNotExist
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		file, err := os.Create(dest)
		if err != nil {
			return err
		}
		defer file.Close()
		_, err = io.Copy(file, response.Body)
		return err
	}

	for _, testCase := range cases {
		t.Run(testCase.version, func(t *testing.T) {
			dir := t.TempDir()
			installerPath := filepath.Join(dir, "installer.jar")
			url := "https://maven.minecraftforge.net/net/minecraftforge/forge/" + testCase.version + "/forge-" + testCase.version + "-installer.jar"
			if err := download(context.Background(), url, installerPath, ""); err != nil {
				t.Fatalf("установщик %s не скачался: %v", testCase.version, err)
			}

			installer, err := Open(installerPath)
			if err != nil {
				t.Fatal(err)
			}
			if installer.Legacy() != testCase.legacy {
				t.Fatalf("Legacy() = %v, want %v", installer.Legacy(), testCase.legacy)
			}
			if installer.MinecraftVersion() != testCase.mcVersion {
				t.Fatalf("MinecraftVersion = %q, want %q", installer.MinecraftVersion(), testCase.mcVersion)
			}
			if installer.MainClass() != testCase.mainClass {
				t.Fatalf("MainClass = %q, want %q", installer.MainClass(), testCase.mainClass)
			}

			launch, err := installer.Install(context.Background(), Request{
				LibrariesDir: filepath.Join(dir, "libraries"),
				ProfileDir:   dir,
				MinecraftJar: filepath.Join(dir, "client.jar"),
				Download:     download,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(launch.Libraries) < 5 {
				t.Fatalf("libraries = %v", launch.Libraries)
			}
			for _, library := range launch.Libraries {
				path := filepath.Join(dir, "libraries", filepath.FromSlash(library))
				info, err := os.Stat(path)
				if err != nil {
					t.Fatalf("%s обещан в classpath, но не скачан: %v", library, err)
				}
				if info.Size() == 0 {
					t.Fatalf("%s скачан пустым", library)
				}
			}
			if len(launch.GameArgs) != 2 || launch.GameArgs[0] != "--tweakClass" {
				t.Fatalf("GameArgs = %v", launch.GameArgs)
			}
			t.Logf("%s: %d библиотек, tweak %s", testCase.version, len(launch.Libraries), launch.GameArgs[1])
		})
	}
}
