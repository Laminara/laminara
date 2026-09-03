package prepare

import (
	"strings"
	"testing"
)

func TestLoaderLibraryReplacesTheVanillaOne(t *testing.T) {
	vanilla := []string{
		"libraries/com/google/guava/guava/15.0/guava-15.0.jar",
		"libraries/org/apache/commons/commons-lang3/3.1/commons-lang3-3.1.jar",
		"libraries/com/mojang/authlib/1.5.21/authlib-1.5.21.jar",
	}
	forge := []string{
		"libraries/com/google/guava/guava/17.0/guava-17.0.jar",
		"libraries/org/apache/commons/commons-lang3/3.3.2/commons-lang3-3.3.2.jar",
		"libraries/net/minecraftforge/forge/1.7.10-10.13.4.1614/forge-universal.jar",
	}

	merged := mergeUnique(vanilla, forge)

	for artifact, want := range map[string]string{
		"guava":           "guava-17.0.jar",
		"commons-lang3":   "commons-lang3-3.3.2.jar",
		"authlib":         "authlib-1.5.21.jar",
		"forge-universal": "forge-universal.jar",
	} {
		var found []string
		for _, path := range merged {
			if strings.Contains(path, artifact+"/") || strings.HasSuffix(path, want) {
				found = append(found, path)
			}
		}
		if len(found) != 1 {
			t.Fatalf("%s попал в classpath %d раз: %v — две версии одной библиотеки означают, что победит первая", artifact, len(found), found)
		}
		if !strings.HasSuffix(found[0], want) {
			t.Fatalf("%s = %s, а нужна %s", artifact, found[0], want)
		}
	}
}

func TestArtifactOfDropsVersionAndFile(t *testing.T) {
	got := artifactOf("libraries/com/google/guava/guava/15.0/guava-15.0.jar")
	if got != "libraries/com/google/guava/guava" {
		t.Fatalf("artifactOf = %q", got)
	}
}
