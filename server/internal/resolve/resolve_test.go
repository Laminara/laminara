package resolve_test

import (
	"testing"

	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/resolve"
)

func TestEvaluateRules(t *testing.T) {
	cases := []struct {
		name  string
		rules []mojang.Rule
		os    string
		want  bool
	}{
		{"no rules", nil, "windows", true},
		{"allow only", []mojang.Rule{{Action: "allow"}}, "linux", true},
		{
			"allow all then disallow osx (on osx)",
			[]mojang.Rule{{Action: "allow"}, {Action: "disallow", OS: &mojang.OSRule{Name: "osx"}}},
			"osx", false,
		},
		{
			"allow all then disallow osx (on linux)",
			[]mojang.Rule{{Action: "allow"}, {Action: "disallow", OS: &mojang.OSRule{Name: "osx"}}},
			"linux", true,
		},
		{"allow only on windows (on linux)", []mojang.Rule{{Action: "allow", OS: &mojang.OSRule{Name: "windows"}}}, "linux", false},
	}
	for _, tc := range cases {
		if got := resolve.EvaluateRules(tc.rules, tc.os, "x86_64"); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

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
