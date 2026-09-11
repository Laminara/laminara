package mojang_test

import (
	"testing"

	"github.com/laminara/laminara/server/internal/mojang"
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
		{"feature rules never match a plain build", []mojang.Rule{{Action: "allow", Features: map[string]bool{"is_demo_user": true}}}, "linux", false},
	}
	for _, tc := range cases {
		if got := mojang.EvaluateRules(tc.rules, tc.os, "x86_64"); got != tc.want {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestARuleAboutTheOSVersionIsNotDecidedAtBuildTime(t *testing.T) {
	windowsTen := []mojang.Rule{{Action: "allow", OS: &mojang.OSRule{Name: "windows", Version: `^10\.`}}}
	if mojang.EvaluateRules(windowsTen, "windows", "x86_64") {
		t.Fatal("версия ОС игрока на сервере неизвестна: такой аргумент нельзя вписывать в сборку, иначе Windows 11 получит -Dos.name=Windows 10")
	}
	plainWindows := []mojang.Rule{{Action: "allow", OS: &mojang.OSRule{Name: "windows"}}}
	if !mojang.EvaluateRules(plainWindows, "windows", "x86_64") {
		t.Fatal("правило без версии обязано работать как раньше")
	}
}
