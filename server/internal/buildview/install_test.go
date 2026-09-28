package buildview

import (
	"testing"

	"github.com/laminara/laminara/server/internal/manifest"
)

func TestInstallCommandRepeatsTheRecordedInstall(t *testing.T) {
	cases := []struct {
		name     string
		settings manifest.Settings
		want     string
	}{
		{name: "nothing recorded", settings: manifest.Settings{}, want: ""},
		{name: "plain", settings: manifest.Settings{Install: "1.21.1 loader=fabric loaderVersion=0.16.5"}, want: "install pack 1.21.1 loader=fabric loaderVersion=0.16.5"},
		{name: "recipe", settings: manifest.Settings{Install: "1.7.10 compat=lwjgl3ify:3.0.33", Compat: "lwjgl3ify:3.0.32"}, want: "install pack 1.7.10 compat=lwjgl3ify:3.0.33"},
		{name: "recipe left behind", settings: manifest.Settings{Install: "1.7.10 loader=forge loaderVersion=10.13.4.1614", Compat: "lwjgl3ify:3.0.33"}, want: "install pack 1.7.10 loader=forge loaderVersion=10.13.4.1614 compat=" + RecipeOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := InstallCommand("pack", c.settings); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
