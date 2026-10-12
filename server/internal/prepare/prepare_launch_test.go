package prepare

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/resolve"
)

func TestInstallerKeepsPatchedClientOutOfBootstrapClasspath(t *testing.T) {
	for _, name := range []string{"neoforge", "forge"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			original := "versions/1.21.1/1.21.1.jar"
			patched := "libraries/net/" + name + "/client-patched.jar"
			universal := "libraries/net/" + name + "/universal.jar"
			bootstrap := "libraries/cpw/mods/bootstraplauncher/bootstraplauncher.jar"
			profile := &resolve.Profile{
				MainClass: "net.minecraft.client.main.Main",
				ClientJar: resolve.Artifact{Path: original},
			}
			install := &loader.InstallResult{
				MainClass: "cpw.mods.bootstraplauncher.BootstrapLauncher",
				ClientJar: patched,
				Libraries: []string{bootstrap},
				OnDisk:    []string{universal, patched},
				JVMArgs:   []string{"-DignoreList=client-extra,${version_name}.jar"},
			}
			opts := Options{ProfileDir: root, PlatformDir: filepath.Join(root, "platforms", "windows-x64"), OS: "windows", PlatformKey: "windows-x64"}
			if err := new(Preparer).writeLaunchProfile(opts, profile, "java-runtime-delta", 21, "runtime/windows-x64/bin/javaw.exe", "1.21.1", install); err != nil {
				t.Fatal(err)
			}
			launch, err := manifest.ReadLaunchProfile(opts.PlatformDir)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(launch.Classpath, original) || !slices.Contains(launch.Classpath, bootstrap) {
				t.Fatalf("bootstrap classpath = %v", launch.Classpath)
			}
			if slices.Contains(launch.Classpath, patched) || slices.Contains(launch.Classpath, universal) {
				t.Fatalf("loader-owned modules reached bootstrap classpath: %v", launch.Classpath)
			}
			if !slices.Contains(launch.ExtraLibraries, patched) || !slices.Contains(launch.ExtraLibraries, universal) {
				t.Fatalf("loader-owned libraries missing from publication: %v", launch.ExtraLibraries)
			}
			if launch.MainClass != install.MainClass || launch.ClientJar != patched {
				t.Fatalf("installer launch metadata = %+v", launch)
			}
		})
	}
}
