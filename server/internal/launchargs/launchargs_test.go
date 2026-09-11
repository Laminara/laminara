package launchargs_test

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/laminara/laminara/server/internal/launchargs"
	"github.com/laminara/laminara/server/internal/mojang"
)

func parse(t *testing.T, raw string) []mojang.Argument {
	t.Helper()
	var arguments []mojang.Argument
	if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
		t.Fatalf("разбор аргументов: %v", err)
	}
	return arguments
}

func TestSelfContainedManifestLosesWhatTheLauncherSuppliesItself(t *testing.T) {
	jvm := parse(t, `[
		{"rules": [{"action": "allow", "os": {"name": "windows"}}], "value": "-XX:HeapDumpPath=MojangTricks"},
		{"rules": [{"action": "allow", "os": {"arch": "x86"}}], "value": "-Xss1M"},
		{"rules": [{"action": "allow", "os": {"name": "osx"}}], "value": ["-XstartOnFirstThread"]},
		"-Djava.library.path=${natives_directory}",
		"-cp",
		"${classpath}",
		"-Dfile.encoding=UTF-8",
		"-Djava.system.class.loader=com.gtnewhorizons.retrofuturabootstrap.RfbSystemClassLoader",
		"--add-opens",
		"java.base/java.lang=ALL-UNNAMED"
	]`)

	got := launchargs.JVM(jvm, "linux", "x86_64")
	want := []string{
		"-Dfile.encoding=UTF-8",
		"-Djava.system.class.loader=com.gtnewhorizons.retrofuturabootstrap.RfbSystemClassLoader",
		"--add-opens",
		"java.base/java.lang=ALL-UNNAMED",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("jvm = %q, want %q", got, want)
	}
}

func TestNativesDirectorySurvivesBecauseTheCoreResolvesIt(t *testing.T) {
	jvm := parse(t, `["-Djna.tmpdir=${natives_directory}", "-Dio.netty.native.workdir=${natives_directory}", "-Dminecraft.launcher.brand=${launcher_name}"]`)
	got := launchargs.JVM(jvm, "linux", "x86_64")
	want := []string{"-Djna.tmpdir=${natives_directory}", "-Dio.netty.native.workdir=${natives_directory}"}
	if !slices.Equal(got, want) {
		t.Fatalf("jvm = %q, want %q", got, want)
	}
}

func TestWindowsAndX86KeepTheirOwnFlags(t *testing.T) {
	jvm := parse(t, `[
		{"rules": [{"action": "allow", "os": {"name": "windows"}}], "value": "-XX:HeapDumpPath=MojangTricks"},
		{"rules": [{"action": "allow", "os": {"arch": "x86"}}], "value": "-Xss1M"}
	]`)
	got := launchargs.JVM(jvm, "windows", "x86")
	want := []string{"-XX:HeapDumpPath=MojangTricks", "-Xss1M"}
	if !slices.Equal(got, want) {
		t.Fatalf("jvm = %q, want %q", got, want)
	}
	if got := launchargs.JVM(jvm, "linux", "x86_64"); got != nil {
		t.Fatalf("на linux/x86_64 не должно остаться ничего, получено %q", got)
	}
}

func TestGameArgumentsKeepOnlyWhatTheLauncherDoesNotSend(t *testing.T) {
	game := parse(t, `[
		"--username", "${auth_player_name}",
		"--version", "${version_name}",
		"--assetIndex", "${assets_index_name}",
		"--userType", "${user_type}",
		"--tweakClass", "cpw.mods.fml.common.launcher.FMLTweaker",
		{"rules": [{"action": "allow", "features": {"is_demo_user": true}}], "value": "--demo"},
		{"rules": [{"action": "allow", "features": {"has_custom_resolution": true}}], "value": ["--width", "${resolution_width}"]}
	]`)

	got := launchargs.Game(game, "linux", "x86_64")
	want := []string{"--tweakClass", "cpw.mods.fml.common.launcher.FMLTweaker"}
	if !slices.Equal(got, want) {
		t.Fatalf("game = %q, want %q", got, want)
	}
}

func TestLegacyArgumentStringKeepsTheTweakClassAndDropsTheVanillaSet(t *testing.T) {
	legacy := "--username ${auth_player_name} --version ${version_name} --gameDir ${game_directory} " +
		"--assetsDir ${assets_root} --assetIndex ${assets_index_name} --uuid ${auth_uuid} " +
		"--accessToken ${auth_access_token} --userType ${user_type} " +
		"--tweakClass net.minecraftforge.fml.common.launcher.FMLTweaker --versionType Forge"

	got := launchargs.Legacy(legacy)
	want := []string{"--tweakClass", "net.minecraftforge.fml.common.launcher.FMLTweaker"}
	if !slices.Equal(got, want) {
		t.Fatalf("legacy = %q, want %q", got, want)
	}
}

func TestLoaderArgumentsWithPlaceholdersTheCoreResolvesAreKept(t *testing.T) {
	jvm := parse(t, `[
		"-DlibraryDirectory=${library_directory}",
		"-p",
		"${library_directory}/cpw/mods/bootstraplauncher/2.0.2/bootstraplauncher-2.0.2.jar${classpath_separator}${library_directory}/x.jar",
		"--add-modules",
		"ALL-MODULE-PATH"
	]`)
	got := launchargs.JVM(jvm, "linux", "x86_64")
	if len(got) != 5 {
		t.Fatalf("аргументы загрузчика потерялись: %q", got)
	}
}

func TestAFlagLosesItsValueTogetherWithItself(t *testing.T) {
	legacy := "--username ${auth_player_name} --session ${auth_session} --version ${version_name} " +
		"--gameDir ${game_directory} --assetsDir ${game_assets}"

	got := launchargs.Legacy(legacy)
	if slices.Contains(got, "--session") {
		t.Fatalf("--session остался без значения: игра падает с «Option --session requires an argument», получено %q", got)
	}
	if got != nil {
		t.Fatalf("у 1.6.4 своих аргументов сверх лаунчерных нет, получено %q", got)
	}
}

func TestAPairTheLauncherCannotFillIsDroppedWhole(t *testing.T) {
	game := parse(t, `["--quickPlaySingleplayer", "${quickPlaySingleplayer}", "--tweakClass", "cpw.mods.fml.common.launcher.FMLTweaker"]`)
	got := launchargs.Game(game, "linux", "x86_64")
	want := []string{"--tweakClass", "cpw.mods.fml.common.launcher.FMLTweaker"}
	if !slices.Equal(got, want) {
		t.Fatalf("game = %q, want %q", got, want)
	}
}
