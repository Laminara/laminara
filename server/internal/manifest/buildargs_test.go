package manifest

import "testing"

func TestBuildArgsAcceptPairedOptions(t *testing.T) {
	args := []string{
		"-Dfile.encoding=UTF-8",
		"--enable-native-access", "ALL-UNNAMED",
		"--add-opens", "java.base/java.lang=ALL-UNNAMED",
		"--add-modules", "jdk.dynalink",
		"-javaagent:libraries/patches/agent.jar",
	}
	if err := validateBuildArgs(args, nil, nil); err != nil {
		t.Fatalf("аргументы из java9args должны приниматься как есть: %v", err)
	}
}

func TestBuildArgsRejectStrayValue(t *testing.T) {
	if err := validateBuildArgs([]string{"-Xmx4G", "java.base/java.lang=ALL-UNNAMED"}, nil, nil); err == nil {
		t.Fatal("значение без своей опции стало бы главным классом — это должно отвергаться")
	}
}

func TestBuildArgsRejectDanglingOption(t *testing.T) {
	if err := validateBuildArgs([]string{"--add-opens"}, nil, nil); err == nil {
		t.Fatal("опция без значения должна отвергаться")
	}
}

func TestBuildArgsRejectClasspathOverride(t *testing.T) {
	for _, arg := range []string{"-cp", "-Djava.class.path=/etc/passwd", "-Djava.library.path=/tmp"} {
		if err := validateBuildArgs([]string{arg, "value"}, nil, nil); err == nil {
			t.Fatalf("%q должен быть запрещён — classpath собирает лаунчер", arg)
		}
	}
}

func TestBuildArgsRejectReservedGameArg(t *testing.T) {
	if err := validateBuildArgs(nil, []string{"--accessToken", "stolen"}, nil); err == nil {
		t.Fatal("подмена токена должна отвергаться")
	}
}

func TestBuildArgsCheckClasspathEntries(t *testing.T) {
	if err := validateBuildArgs(nil, nil, []string{"../../etc/passwd"}); err == nil {
		t.Fatal("выход за пределы сборки должен отвергаться")
	}
	if err := validateBuildArgs(nil, nil, []string{"libraries/lwjgl3ify/patches.jar"}); err != nil {
		t.Fatalf("обычный путь внутри сборки должен приниматься: %v", err)
	}
}
