package command

import (
	"reflect"
	"testing"
)

func TestArgumentsRoundTrip(t *testing.T) {
	for _, args := range [][]string{
		{"install", "My Pack", "1.21.1"},
		{"delete", "Сборка 'один' \"два\""},
		{"settings", "branding.tagline", ""},
		{"settings", "modules.config.demo", `{"label":"two  spaces","path":"C:\\new\\mods"}`},
		{"settings", "branding.logoPath", `C:\new\mods`},
		{"settings", "branding.logoPath", `\\server\share`},
		{"echo", "line\nnext\tword", "[value]"},
	} {
		t.Run(Format(args...), func(t *testing.T) {
			got, err := Split(Format(args...))
			if err != nil || !reflect.DeepEqual(got, args) {
				t.Fatalf("round trip: %q, %v; want %q", got, err, args)
			}
		})
	}
}

func TestRawJSONAndPathsRemainLiteral(t *testing.T) {
	for _, test := range []struct {
		line string
		want []string
	}{
		{`settings modules.config.demo {"values": ["one", "two words" ], "name": "a  b"}`, []string{"settings", "modules.config.demo", `{"values": ["one", "two words" ], "name": "a  b"}`}},
		{`echo C:\new\mods \\server\share`, []string{"echo", `C:\new\mods`, `\\server\share`}},
		{`publish 'My Pack'`, []string{"publish", "My Pack"}},
	} {
		got, err := Split(test.line)
		if err != nil || !reflect.DeepEqual(got, test.want) {
			t.Fatalf("%s: %q, %v", test.line, got, err)
		}
	}
}

func TestInvalidQuotesNeverEchoArguments(t *testing.T) {
	for _, line := range []string{`settings token "fixture-secret`, `echo "fixture-secret"tail`, `echo "\qfixture-secret"`} {
		if _, err := Split(line); err == nil {
			t.Fatalf("invalid command accepted")
		}
	}
}
