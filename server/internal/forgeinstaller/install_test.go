package forgeinstaller

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeJar(t *testing.T, path string, files map[string]string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestResolveToken(t *testing.T) {
	placeholders := map[string]string{"ROOT": "/profile", "MINECRAFT_JAR": "/mc/client.jar"}
	libraries := "/libs"

	if got, _ := resolveToken("{ROOT}/run.sh", placeholders, libraries); got != "/profile/run.sh" {
		t.Fatalf("embedded placeholder = %q", got)
	}
	if got, _ := resolveToken("{MINECRAFT_JAR}", placeholders, libraries); got != "/mc/client.jar" {
		t.Fatalf("whole placeholder = %q", got)
	}
	if got, _ := resolveToken("[net.neoforged:neoform:1.21.1:mappings@txt]", placeholders, libraries); got != filepath.FromSlash("/libs/net/neoforged/neoform/1.21.1/neoform-1.21.1-mappings.txt") {
		t.Fatalf("maven token = %q", got)
	}
	if got, _ := resolveToken("--task", placeholders, libraries); got != "--task" {
		t.Fatalf("literal = %q", got)
	}
}

const sampleInstallProfile = `{
  "spec": 1, "minecraft": "1.21.1", "version": "neoforge-21.1.235", "json": "/version.json",
  "data": { "BINPATCH": { "client": "/data/client.lzma", "server": "/data/server.lzma" } },
  "processors": [
    { "sides": ["server"], "jar": "z:only:1", "classpath": [], "args": ["skip"] },
    { "sides": ["client"], "jar": "com.example:proc:2", "classpath": ["com.example:cp:3"], "args": ["--input", "{MINECRAFT_JAR}", "--patch", "{BINPATCH}", "--out", "{ROOT}/patched.jar"] }
  ],
  "libraries": [ { "name": "com.example:proc:2", "downloads": { "artifact": { "path": "com/example/proc/2/proc-2.jar", "sha1": "", "size": 1, "url": "https://example/proc-2.jar" } } } ]
}`

const sampleVersion = `{
  "id": "neoforge-21.1.235", "inheritsFrom": "1.21.1",
  "mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
  "arguments": { "jvm": ["-p", "modules"], "game": ["--fml.mcVersion", "1.21.1"] },
  "libraries": [ { "name": "net.neoforged:neoforge:21.1.235", "downloads": { "artifact": { "url": "https://example/neoforge.jar" } } } ]
}`

func TestOpenAndLibraries(t *testing.T) {
	jarPath := filepath.Join(t.TempDir(), "installer.jar")
	writeJar(t, jarPath, map[string]string{
		"install_profile.json": sampleInstallProfile,
		"version.json":         sampleVersion,
		"data/client.lzma":     "binary-patch",
		"maven/com/example/embedded/1/embedded-1.jar": "embedded-artifact",
	})

	installer, err := Open(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	if installer.MinecraftVersion() != "1.21.1" || installer.InheritsFrom() != "1.21.1" {
		t.Fatalf("versions = %s / %s", installer.MinecraftVersion(), installer.InheritsFrom())
	}
	if installer.MainClass() != "cpw.mods.bootstraplauncher.BootstrapLauncher" {
		t.Fatalf("mainClass = %s", installer.MainClass())
	}
	if len(installer.Libraries()) != 2 {
		t.Fatalf("libraries = %d", len(installer.Libraries()))
	}

	librariesDir := t.TempDir()
	if err := installer.extractEmbeddedMaven(librariesDir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(librariesDir, "com/example/embedded/1/embedded-1.jar")); err != nil {
		t.Fatalf("embedded maven not extracted: %v", err)
	}

	dataDir := t.TempDir()
	extracted, err := installer.extractFile("/data/client.lzma", dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if content, _ := os.ReadFile(extracted); string(content) != "binary-patch" {
		t.Fatalf("extracted data = %q", content)
	}
}

func TestProcessorCommandSkipsServerAndBuildsClient(t *testing.T) {
	jarPath := filepath.Join(t.TempDir(), "installer.jar")
	writeJar(t, jarPath, map[string]string{"install_profile.json": sampleInstallProfile, "version.json": sampleVersion})
	installer, err := Open(jarPath)
	if err != nil {
		t.Fatal(err)
	}

	librariesDir := t.TempDir()
	processorJar := filepath.Join(librariesDir, "com/example/proc/2/proc-2.jar")
	writeJar(t, processorJar, map[string]string{"META-INF/MANIFEST.MF": "Manifest-Version: 1.0\r\nMain-Class: com.example.Processor\r\n"})

	if includesClient(installer.profile.Processors[0].Sides) {
		t.Fatal("server-only processor must be skipped")
	}

	placeholders := map[string]string{"MINECRAFT_JAR": "/mc/client.jar", "ROOT": "/profile", "BINPATCH": "/data/client.lzma", "SIDE": "client"}
	command, err := installer.processorCommand(installer.profile.Processors[1], placeholders, Request{LibrariesDir: librariesDir, JavaBin: "java"})
	if err != nil {
		t.Fatal(err)
	}
	if command[0] != "java" || command[1] != "-cp" || command[3] != "com.example.Processor" {
		t.Fatalf("command prefix = %v", command[:4])
	}
	if !strings.Contains(command[2], processorJar) || !strings.Contains(command[2], filepath.FromSlash("com/example/cp/3/cp-3.jar")) {
		t.Fatalf("classpath = %q", command[2])
	}
	joined := strings.Join(command[4:], " ")
	if joined != "--input /mc/client.jar --patch /data/client.lzma --out /profile/patched.jar" {
		t.Fatalf("args = %q", joined)
	}
}

const profileWithToolLibraries = `{
  "spec": 1, "minecraft": "1.20.1", "version": "forge-47.4.23", "json": "/version.json",
  "data": {}, "processors": [],
  "libraries": [
    { "name": "net.sf.jopt-simple:jopt-simple:6.0-alpha-3", "downloads": { "artifact": { "path": "net/sf/jopt-simple/jopt-simple/6.0-alpha-3/jopt-simple-6.0-alpha-3.jar", "url": "https://example/jopt6.jar" } } },
    { "name": "net.minecraftforge:installertools:1.4.1", "downloads": { "artifact": { "path": "net/minecraftforge/installertools/1.4.1/installertools-1.4.1.jar", "url": "https://example/tools.jar" } } }
  ]
}`

const versionWithGameLibraries = `{
  "id": "forge-47.4.23", "inheritsFrom": "1.20.1",
  "mainClass": "cpw.mods.bootstraplauncher.BootstrapLauncher",
  "arguments": { "jvm": ["-p", "modules"], "game": [] },
  "libraries": [ { "name": "net.minecraftforge:forge:1.20.1-47.4.23", "downloads": { "artifact": { "url": "https://example/forge.jar" } } } ]
}`

func TestToolsOfTheInstallerStayOutOfTheGameClasspath(t *testing.T) {
	jarPath := filepath.Join(t.TempDir(), "installer.jar")
	writeJar(t, jarPath, map[string]string{
		"install_profile.json": profileWithToolLibraries,
		"version.json":         versionWithGameLibraries,
	})

	installer, err := Open(jarPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(installer.Libraries()) != 3 {
		t.Fatalf("скачать нужно всё: и инструменты установщика, и библиотеки игры, а вышло %d", len(installer.Libraries()))
	}

	runtime := installer.RuntimeLibraries()
	if len(runtime) != 1 || runtime[0].Name != "net.minecraftforge:forge:1.20.1-47.4.23" {
		t.Fatalf("в classpath игры попало лишнее: %+v", runtime)
	}
	for _, library := range runtime {
		if strings.Contains(library.Name, "jopt-simple") {
			t.Fatal("jopt-simple установщика перекроет ванильный, и modlauncher не найдёт модуль jopt.simple")
		}
	}
}

func TestWhatTheProcessorsProducedInsideLibrariesIsKept(t *testing.T) {
	libraries := t.TempDir()
	outside := t.TempDir()

	patched := filepath.Join(libraries, "net", "minecraftforge", "forge", "1", "forge-1-client.jar")
	srg := filepath.Join(libraries, "net", "minecraft", "client", "1", "client-1-srg.jar")
	for _, path := range []string{patched, srg} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("артефакт"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stranger := filepath.Join(outside, "installer.jar")
	if err := os.WriteFile(stranger, []byte("установщик"), 0o644); err != nil {
		t.Fatal(err)
	}

	produced := producedInLibraries(map[string]string{
		"PATCHED":   patched,
		"MC_SRG":    srg,
		"INSTALLER": stranger,
		"SIDE":      "client",
		"MISSING":   filepath.Join(libraries, "нет", "такого.jar"),
	}, libraries)

	want := []string{
		"net/minecraft/client/1/client-1-srg.jar",
		"net/minecraftforge/forge/1/forge-1-client.jar",
	}
	if strings.Join(produced, " ") != strings.Join(want, " ") {
		t.Fatalf("в сборку попадёт %v — FML собирает игру из этих файлов по пути, и без них она не стартует", produced)
	}
}
