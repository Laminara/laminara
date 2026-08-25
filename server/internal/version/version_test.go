package version_test

import (
	"testing"

	"github.com/laminara/laminara/server/internal/version"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		left  string
		right string
		want  int
	}{
		{"1.2.0", "1.2.0", 0},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.1", "1.2.0", 1},
		{"1.3.0", "1.2.9", 1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.0", "1.2.1", -1},
		{"1.2", "1.2.0", 0},
		{"1.2.0", "1.2.0-rc1", 1},
		{"1.2.0-rc1", "1.2.0-rc2", -1},
		{"0.1.0", "0.0.0-dev", 1},
		{"1.2.0+build7", "1.2.0", 0},
	}

	for _, c := range cases {
		if got := version.Compare(c.left, c.right); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.left, c.right, got, c.want)
		}
	}
}

func TestDevBuildAcceptsAnyRelease(t *testing.T) {
	if !version.IsNewer("0.1.0", "0.0.0-dev") {
		t.Fatal("a release must count as newer than a development build")
	}
	if version.IsNewer("0.1.0", "0.1.0") {
		t.Fatal("the same version must not count as an update")
	}
	if version.IsNewer("0.0.9", "0.1.0") {
		t.Fatal("an older release must not count as an update")
	}
}
