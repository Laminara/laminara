package buildspec

import "testing"

func TestUntrustedPaths(t *testing.T) {
	for _, path := range []string{"../secret", "/absolute", "a/../b", `C:\secret`, ".laminara/current.json", "mods/.hidden", "runtime/java", "platforms/linux/a", "laminara.settings.json", "mods/x\x00.jar"} {
		if SafePath(path) {
			t.Errorf("accepted %q", path)
		}
	}
	if !SafePath("mods/Мод с пробелом.jar") {
		t.Fatal("valid relative path rejected")
	}
}

func TestDescriptionRequiresPinsAndRejectsUnknownFields(t *testing.T) {
	for _, raw := range []string{
		`{"version":1,"name":"pack","minecraft":"latest","loader":"vanilla","compat":"off","platforms":["linux"],"sourceRevision":"abc"}`,
		`{"version":1,"name":"pack","minecraft":"1.21.1","loader":"vanilla","compat":"off","platforms":["linux"],"sourceRevision":"abc","typo":1}`,
		`{"version":1,"name":"pack","minecraft":"1.21.1","loader":"vanilla","compat":"lwjgl3ify:latest","platforms":["linux"],"sourceRevision":"abc"}`,
	} {
		if _, err := Decode([]byte(raw)); err == nil {
			t.Fatal("invalid specification accepted")
		}
	}
}
