package pathpolicy

import (
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"options.txt", "options.txt", true},
		{"options.txt", "config/options.txt", false},
		{"config/", "config/foo.json", true},
		{"config/", "config", true},
		{"config/", "configs/foo", false},
		{"config/**", "config/a/b/c.json", true},
		{"saves/**", "saves", true},
		{"screenshots/*.png", "screenshots/a.png", true},
		{"screenshots/*.png", "screenshots/sub/a.png", false},
		{"**/*.log", "logs/latest.log", true},
		{"**/*.log", "a/b/c.log", true},
		{"**/mymods/**", "x/mymods/y.jar", true},
		{"config/anticheat/**", "config/anticheat/rules.toml", true},
		{"config/anticheat/**", "config/other.toml", false},
	}
	for _, c := range cases {
		if got := Match(c.pattern, c.path); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.path, got, c.want)
		}
	}
}

func TestResolvePrecedence(t *testing.T) {
	uw := []string{"config/"}
	enf := []string{"config/anticheat/**"}
	if got := Resolve("config/anticheat/rules.toml", uw, enf); got != corev1.FilePolicy_FILE_POLICY_ENFORCED {
		t.Errorf("enforced must win inside a user_writable dir, got %v", got)
	}
	if got := Resolve("config/options.json", uw, enf); got != corev1.FilePolicy_FILE_POLICY_USER_WRITABLE {
		t.Errorf("expected user_writable, got %v", got)
	}
	if got := Resolve("mods/foo.jar", uw, enf); got != corev1.FilePolicy_FILE_POLICY_UNSPECIFIED {
		t.Errorf("expected default, got %v", got)
	}
}
