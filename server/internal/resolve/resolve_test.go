package resolve_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/resolve"
)

func TestResolveLibrariesNativesLoader(t *testing.T) {
	detail := &mojang.VersionDetail{
		ID:          "1.21.1",
		MainClass:   "net.minecraft.client.main.Main",
		JavaVersion: mojang.JavaVersion{Component: "java-runtime-delta", MajorVersion: 21},
		AssetIndex:  mojang.AssetIndexRef{ID: "17", URL: "https://example/assets.json"},
	}
	detail.Downloads.Client = mojang.Download{SHA1: "cc", Size: 500, URL: "https://example/client.jar"}
	detail.Libraries = []mojang.Library{
		{
			Name:      "com.example:common:1.0",
			Downloads: mojang.LibraryDownloads{Artifact: &mojang.Artifact{Path: "com/example/common/1.0/common-1.0.jar", SHA1: "a1", Size: 10, URL: "https://libs/common.jar"}},
		},
		{
			Name:      "com.example:winonly:1.0",
			Rules:     []mojang.Rule{{Action: "allow", OS: &mojang.OSRule{Name: "windows"}}},
			Downloads: mojang.LibraryDownloads{Artifact: &mojang.Artifact{Path: "com/example/winonly/1.0/winonly-1.0.jar", URL: "https://libs/winonly.jar"}},
		},
		{
			Name:    "org.lwjgl:lwjgl:3",
			Natives: map[string]string{"linux": "natives-linux"},
			Downloads: mojang.LibraryDownloads{Classifiers: map[string]*mojang.Artifact{
				"natives-linux": {Path: "org/lwjgl/lwjgl/3/lwjgl-3-natives-linux.jar", URL: "https://libs/lwjgl-natives-linux.jar"},
			}},
		},
	}

	loaderProfile := &loader.LoaderProfile{
		MainClass: "net.fabricmc.loader.impl.launch.knot.KnotClient",
		Libraries: []loader.Library{{Name: "net.fabricmc:fabric-loader:0.16.0", URL: "https://maven.fabricmc.net/"}},
	}

	profile, err := resolve.Resolve(detail, "linux", "x86_64", loaderProfile)
	if err != nil {
		t.Fatal(err)
	}
	if profile.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("mainClass = %q (loader override failed)", profile.MainClass)
	}
	if profile.JavaMajor != 21 {
		t.Fatalf("java major = %d", profile.JavaMajor)
	}
	if profile.ClientJar.Path != "versions/1.21.1/1.21.1.jar" {
		t.Fatalf("client jar path = %q", profile.ClientJar.Path)
	}
	if len(profile.Libraries) != 2 {
		t.Fatalf("libraries = %+v", profile.Libraries)
	}
	if profile.Libraries[1].Path != "libraries/net/fabricmc/fabric-loader/0.16.0/fabric-loader-0.16.0.jar" {
		t.Fatalf("fabric loader maven path = %q", profile.Libraries[1].Path)
	}
	if profile.Libraries[1].URL != "https://maven.fabricmc.net/net/fabricmc/fabric-loader/0.16.0/fabric-loader-0.16.0.jar" {
		t.Fatalf("fabric loader url = %q", profile.Libraries[1].URL)
	}
	if len(profile.Natives) != 1 || profile.Natives[0].Path != "libraries/org/lwjgl/lwjgl/3/lwjgl-3-natives-linux.jar" {
		t.Fatalf("natives = %+v", profile.Natives)
	}
}

func TestSelfContainedRecipeManifestCarriesItsOwnLoaderAndArguments(t *testing.T) {
	raw := `{
		"id": "1.7.10-Forge10.13.4.1614-1.7.10-lwjgl3ify-3.0.33",
		"mainClass": "com.gtnewhorizons.retrofuturabootstrap.MainStartOnFirstThread",
		"javaVersion": {"component": "java-runtime-epsilon", "majorVersion": 25},
		"assetIndex": {"id": "1.7.10", "url": "https://example/1.7.10.json"},
		"downloads": {"client": {"sha1": "aa", "url": "https://example/client.jar"}},
		"arguments": {
			"jvm": [
				{"rules": [{"action": "allow", "os": {"name": "windows"}}], "value": "-XX:HeapDumpPath=MojangTricks"},
				"-Djava.library.path=${natives_directory}",
				"-cp",
				"${classpath}",
				"-Djava.system.class.loader=com.gtnewhorizons.retrofuturabootstrap.RfbSystemClassLoader",
				"--add-opens",
				"java.base/java.lang=ALL-UNNAMED"
			],
			"game": [
				"--username", "${auth_player_name}",
				"--tweakClass", "cpw.mods.fml.common.launcher.FMLTweaker"
			]
		},
		"libraries": [
			{"name": "org.lwjgl:lwjgl:3.4.2", "downloads": {"artifact": {"path": "org/lwjgl/lwjgl/3.4.2/lwjgl-3.4.2.jar", "url": "https://example/lwjgl.jar"}}},
			{"name": "org.lwjgl:lwjgl-natives-windows:3.4.2", "rules": [{"action": "allow", "os": {"name": "windows"}}], "downloads": {"artifact": {"path": "org/lwjgl/lwjgl/3.4.2/lwjgl-3.4.2-natives-windows.jar", "url": "https://example/win.jar"}}},
			{"name": "net.minecraftforge:forge:1.7.10-10.13.4.1614-1.7.10:universal", "downloads": {"artifact": {"path": "net/minecraftforge/forge/x/forge-universal.jar", "url": "https://example/forge.jar"}}},
			{"name": "com.github.GTNewHorizons:lwjgl3ify:3.0.33:forgePatches", "downloads": {"artifact": {"sha1": "cc", "url": "https://example/patches.jar"}}}
		]
	}`
	var detail mojang.VersionDetail
	if err := json.Unmarshal([]byte(raw), &detail); err != nil {
		t.Fatal(err)
	}

	profile, err := resolve.Resolve(&detail, "linux", "x86_64", nil)
	if err != nil {
		t.Fatal(err)
	}
	if profile.MainClass != "com.gtnewhorizons.retrofuturabootstrap.MainStartOnFirstThread" {
		t.Errorf("mainClass = %q", profile.MainClass)
	}
	if profile.JavaComponent != "java-runtime-epsilon" || profile.JavaMajor != 25 {
		t.Errorf("java = %q %d", profile.JavaComponent, profile.JavaMajor)
	}
	wantJVM := []string{
		"-Djava.system.class.loader=com.gtnewhorizons.retrofuturabootstrap.RfbSystemClassLoader",
		"--add-opens",
		"java.base/java.lang=ALL-UNNAMED",
	}
	if !slices.Equal(profile.JvmArgs, wantJVM) {
		t.Errorf("jvmArgs = %q, want %q", profile.JvmArgs, wantJVM)
	}
	wantGame := []string{"--tweakClass", "cpw.mods.fml.common.launcher.FMLTweaker"}
	if !slices.Equal(profile.GameArgs, wantGame) {
		t.Errorf("gameArgs = %q, want %q", profile.GameArgs, wantGame)
	}
	if len(profile.Libraries) != 3 {
		t.Fatalf("на linux должны остаться lwjgl, forge и патчи, а не %+v", profile.Libraries)
	}
	if profile.Libraries[1].Path != "libraries/net/minecraftforge/forge/x/forge-universal.jar" {
		t.Errorf("forge не попал в classpath: %+v", profile.Libraries)
	}
	patches := "libraries/com/github/GTNewHorizons/lwjgl3ify/3.0.33/lwjgl3ify-3.0.33-forgePatches.jar"
	if profile.Libraries[2].Path != patches {
		t.Errorf("библиотека без path должна лечь по своим координатам, а легла в %q", profile.Libraries[2].Path)
	}
}
