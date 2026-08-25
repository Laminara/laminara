package forgeinstaller

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const legacyProfileJSON = `{
  "install": {
    "profileName": "forge",
    "target": "1.12.2-forge1.12.2-14.23.5.2860",
    "path": "net.minecraftforge:forge:1.12.2-14.23.5.2860",
    "filePath": "forge-1.12.2-14.23.5.2860-universal.jar",
    "minecraft": "1.12.2"
  },
  "versionInfo": {
    "id": "1.12.2-forge1.12.2-14.23.5.2860",
    "mainClass": "net.minecraft.launchwrapper.Launch",
    "minecraftArguments": "--username ${auth_player_name} --version ${version_name} --tweakClass net.minecraftforge.fml.common.launcher.FMLTweaker",
    "libraries": [
      {"name": "net.minecraftforge:forge:1.12.2-14.23.5.2860", "url": "http://files.minecraftforge.net/maven/"},
      {"name": "org.ow2.asm:asm-debug-all:5.2", "url": "http://files.minecraftforge.net/maven/"},
      {"name": "net.minecraft:launchwrapper:1.12", "serverreq": true},
      {"name": "org.scala-lang:scala-library:2.11.1", "url": "http://files.minecraftforge.net/maven/", "clientreq": false}
    ]
  }
}`

func legacyInstallerJar(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "forge-1.12.2-14.23.5.2860-installer.jar")

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	archive := zip.NewWriter(file)
	for name, body := range map[string]string{
		"install_profile.json":                    legacyProfileJSON,
		"forge-1.12.2-14.23.5.2860-universal.jar": "универсальный jar загрузчика",
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOldInstallerIsRecognised(t *testing.T) {
	dir := t.TempDir()
	installer, err := Open(legacyInstallerJar(t, dir))
	if err != nil {
		t.Fatal(err)
	}
	if !installer.Legacy() {
		t.Fatal("установщик до 1.13 не опознан как старый")
	}
	if installer.MinecraftVersion() != "1.12.2" {
		t.Fatalf("MinecraftVersion = %q", installer.MinecraftVersion())
	}
	if installer.MainClass() != "net.minecraft.launchwrapper.Launch" {
		t.Fatalf("MainClass = %q", installer.MainClass())
	}
}

func TestOldInstallerLaysOutLibraries(t *testing.T) {
	dir := t.TempDir()
	installer, err := Open(legacyInstallerJar(t, dir))
	if err != nil {
		t.Fatal(err)
	}

	var asked []string
	launch, err := installer.Install(context.Background(), Request{
		LibrariesDir: filepath.Join(dir, "libraries"),
		ProfileDir:   dir,
		MinecraftJar: filepath.Join(dir, "client.jar"),
		Download: func(_ context.Context, url, dest, _ string) error {
			asked = append(asked, url)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dest, []byte("библиотека"), 0o644)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	universal := filepath.Join(dir, "libraries", "net", "minecraftforge", "forge", "1.12.2-14.23.5.2860", "forge-1.12.2-14.23.5.2860.jar")
	if _, err := os.Stat(universal); err != nil {
		t.Fatal("jar загрузчика не распакован из установщика")
	}

	if !contains(launch.Libraries, "net/minecraftforge/forge/1.12.2-14.23.5.2860/forge-1.12.2-14.23.5.2860.jar") {
		t.Fatalf("libraries = %v", launch.Libraries)
	}
	for _, library := range launch.Libraries {
		if strings.Contains(library, "scala-library") {
			t.Fatal("библиотека, помеченная clientreq=false, не нужна клиенту")
		}
	}
	if launch.ClientJar != "" {
		t.Fatalf("ClientJar = %q: у старого Forge клиентский jar остаётся ванильным", launch.ClientJar)
	}
	if strings.Join(launch.GameArgs, " ") != "--tweakClass net.minecraftforge.fml.common.launcher.FMLTweaker" {
		t.Fatalf("GameArgs = %v", launch.GameArgs)
	}
}

func TestDeadMavenIsReplaced(t *testing.T) {
	bases := mirrorsFor("http://files.minecraftforge.net/maven/")
	if bases[0] != "https://maven.minecraftforge.net/" {
		t.Fatalf("первым идёт %q, а files.minecraftforge.net давно не отвечает", bases[0])
	}
	if len(bases) < 2 {
		t.Fatal("нужны запасные репозитории на случай, если основной не отдал файл")
	}
}

func TestMirrorsAreTriedInTurn(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "library.jar")

	var tried []string
	err := fetchFromMirrors(context.Background(), Request{
		Download: func(_ context.Context, url, dest, _ string) error {
			tried = append(tried, url)
			if !strings.HasPrefix(url, "https://libraries.minecraft.net/") {
				return os.ErrNotExist
			}
			return os.WriteFile(dest, []byte("библиотека"), 0o644)
		},
	}, "https://maven.minecraftforge.net/", "a/b/c.jar", dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(tried) < 2 {
		t.Fatalf("зеркала не перебирались: %v", tried)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal("файл не сохранён, хотя одно из зеркал его отдало")
	}
}

const modernProfileJSON = `{
  "spec": 0,
  "json": "/version.json",
  "path": "net.minecraftforge:forge:1.12.2-14.23.5.2860",
  "minecraft": "1.12.2",
  "data": {},
  "processors": [],
  "libraries": []
}`

const modernVersionJSON = `{
  "id": "1.12.2-forge-14.23.5.2860",
  "inheritsFrom": "1.12.2",
  "mainClass": "net.minecraft.launchwrapper.Launch",
  "minecraftArguments": "--username ${auth_player_name} --tweakClass net.minecraftforge.fml.common.launcher.FMLTweaker --versionType Forge",
  "libraries": []
}`

func TestOldArgumentsSurviveTheModernInstaller(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "installer.jar")

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for name, body := range map[string]string{"install_profile.json": modernProfileJSON, "version.json": modernVersionJSON} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	file.Close()

	installer, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if installer.Legacy() {
		t.Fatal("установщик с processors — современный, даже если версия старая")
	}

	launch, err := installer.Install(context.Background(), Request{
		LibrariesDir: filepath.Join(dir, "libraries"),
		ProfileDir:   dir,
		MinecraftJar: filepath.Join(dir, "client.jar"),
		Download:     func(context.Context, string, string, string) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(launch.GameArgs, " ") != "--tweakClass net.minecraftforge.fml.common.launcher.FMLTweaker" {
		t.Fatalf("GameArgs = %v: без tweakClass игра запустится без Forge", launch.GameArgs)
	}
}
