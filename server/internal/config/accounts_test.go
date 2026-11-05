package config

import "testing"

func TestAccountLimits(t *testing.T) {
	for _, tc := range []struct {
		name     string
		launcher *LauncherConfig
		want     int
		invalid  bool
	}{
		{name: "absent", want: 1},
		{name: "disabled", launcher: &LauncherConfig{MaxAccounts: 5}, want: 1},
		{name: "default", launcher: &LauncherConfig{MultiAccount: true}, want: 5},
		{name: "one", launcher: &LauncherConfig{MultiAccount: true, MaxAccounts: 1}, want: 1},
		{name: "upper", launcher: &LauncherConfig{MultiAccount: true, MaxAccounts: 20}, want: 20},
		{name: "negative", launcher: &LauncherConfig{MaxAccounts: -1}, invalid: true},
		{name: "too many", launcher: &LauncherConfig{MultiAccount: true, MaxAccounts: 21}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(&Config{Launcher: tc.launcher})
			if (err != nil) != tc.invalid {
				t.Fatalf("Validate = %v", err)
			}
			if !tc.invalid && tc.launcher.AccountLimit() != tc.want {
				t.Fatalf("limit = %d, want %d", tc.launcher.AccountLimit(), tc.want)
			}
		})
	}
}
