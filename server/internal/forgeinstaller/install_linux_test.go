package forgeinstaller

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestInstallProcessorPaths(t *testing.T) {
	for _, name := range []string{"relative", "absolute", "path", "current-data"} {
		t.Run(name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			root := filepath.Join("profiles", "Hi Tech")
			libraries := filepath.Join(root, "libraries")
			java := filepath.Join(root, "platforms", "windows-x64", ".laminara", "buildjre", "linux", "bin", "java")
			if err := os.MkdirAll(filepath.Dir(java), 0o755); err != nil {
				t.Fatal(err)
			}
			script := `#!/bin/sh
set -eu
test "$1" = -cp
classpath=$2
shift 3
oldIFS=$IFS
IFS=:
for jar in $classpath; do
  test -f "$jar"
done
IFS=$oldIFS
while test "$#" -gt 0; do
  kind=$1
  value=$2
  shift 2
  case "$kind" in
    --root) test "$value" = "$PWD" ;;
    --output) printf patched > "$value" ;;
    *) test -f "$value" ;;
  esac
done
`
			if err := os.WriteFile(java, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			installerPath := filepath.Join(root, ".laminara", "installer.jar")
			writeJar(t, installerPath, map[string]string{
				"install_profile.json": `{"spec":1,"minecraft":"1.21.1","json":"/version.json","data":{"PATCH":{"client":"/data/client.lzma"},"PATCHED":{"client":"[com.example:patched:1]"}},"processors":[{"jar":"com.example:proc:2","classpath":["com.example:cp:3"],"args":["--input","{MINECRAFT_JAR}","--installer","{INSTALLER}","--patch","{PATCH}","--root","{ROOT}","--output","{PATCHED}"]}],"libraries":[]}`,
				"version.json":         `{"mainClass":"Processor"}`,
				"data/client.lzma":     "patch",
			})
			writeJar(t, filepath.Join(libraries, "com/example/proc/2/proc-2.jar"), map[string]string{"META-INF/MANIFEST.MF": "Main-Class: Processor\r\n"})
			writeJar(t, filepath.Join(libraries, "com/example/cp/3/cp-3.jar"), nil)
			patched := filepath.Join(libraries, "com/example/patched/1/patched-1.jar")
			if err := os.MkdirAll(filepath.Dir(patched), 0o755); err != nil {
				t.Fatal(err)
			}
			client := filepath.Join(root, "client.jar")
			if err := os.WriteFile(client, []byte("client"), 0o644); err != nil {
				t.Fatal(err)
			}
			req := Request{ProfileDir: root, LibrariesDir: libraries, MinecraftJar: client, JavaBin: java, DataDir: filepath.Join(root, ".laminara", "data")}
			req.Download = func(context.Context, string, string, string) error { return nil }
			if name == "current-data" {
				req.DataDir = ""
			}
			if name == "absolute" {
				for _, path := range []*string{&installerPath, &req.ProfileDir, &req.LibrariesDir, &req.MinecraftJar, &req.JavaBin, &req.DataDir} {
					absolute, err := filepath.Abs(*path)
					if err != nil {
						t.Fatal(err)
					}
					*path = absolute
				}
			}
			if name == "path" {
				javaDir, err := filepath.Abs(filepath.Dir(java))
				if err != nil {
					t.Fatal(err)
				}
				t.Setenv("PATH", javaDir+string(os.PathListSeparator)+os.Getenv("PATH"))
				req.JavaBin = "java"
			}
			installer, err := Open(installerPath)
			if err != nil {
				t.Fatal(err)
			}
			launch, err := installer.Install(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if launch.ClientJar != "com/example/patched/1/patched-1.jar" || len(launch.OnDisk) != 1 || launch.OnDisk[0] != launch.ClientJar {
				t.Fatalf("produced libraries: %+v", launch)
			}
			if content, err := os.ReadFile(patched); err != nil || string(content) != "patched" {
				t.Fatalf("patched client = %q, error = %v", content, err)
			}
		})
	}
}
