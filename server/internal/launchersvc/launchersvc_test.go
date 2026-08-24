package launchersvc

import (
	"testing"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func TestClassifyRecognisesWhatTheBuildScriptProduces(t *testing.T) {
	cases := map[string]struct {
		platform corev1.Platform
		kind     corev1.LauncherArtifactKind
	}{
		"Laminara":                   {corev1.Platform_PLATFORM_LINUX, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE},
		"Laminara.exe":               {corev1.Platform_PLATFORM_WINDOWS_X64, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE},
		"Laminara-linux-arm64":       {corev1.Platform_PLATFORM_LINUX_ARM64, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE},
		"Laminara-setup.exe":         {corev1.Platform_PLATFORM_WINDOWS_X64, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER},
		"Laminara.AppImage":          {corev1.Platform_PLATFORM_LINUX, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_IMAGE},
		"Laminara-mac-os.app.tar.gz": {corev1.Platform_PLATFORM_MAC_OS, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_BUNDLE_TAR_GZ},
	}
	for name, want := range cases {
		platform, kind, ok := classify(name)
		if !ok {
			t.Fatalf("%s was not recognised", name)
		}
		if platform != want.platform || kind != want.kind {
			t.Fatalf("%s = %v/%v, want %v/%v", name, platform, kind, want.platform, want.kind)
		}
	}
}

func TestClassifyRejectsWhatIsNotALauncher(t *testing.T) {
	for _, name := range []string{"notes.txt", "readme.md", "release.json"} {
		if _, _, ok := classify(name); ok {
			t.Fatalf("%s must not be taken for a launcher", name)
		}
	}
}
