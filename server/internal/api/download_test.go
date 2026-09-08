package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/launchersvc"
)

func publishedLauncher(t *testing.T) *launchersvc.Releases {
	t.Helper()
	dir := t.TempDir()
	release := &corev1.LauncherRelease{
		Version: "1.0.0",
		Artifacts: []*corev1.LauncherArtifact{
			{
				Platform: corev1.Platform_PLATFORM_WINDOWS_X64,
				Kind:     corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE,
				FileName: "MagicWorld.exe",
				Object: &corev1.ObjectRef{
					Hash: &corev1.Hash{Algo: corev1.HashAlgo_HASH_ALGO_BLAKE3, Value: []byte{0xab, 0xcd, 0xef}},
					Size: 42,
				},
			},
			{
				Platform: corev1.Platform_PLATFORM_LINUX,
				Kind:     corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE,
				FileName: "magic-world",
				Object: &corev1.ObjectRef{
					Hash: &corev1.Hash{Algo: corev1.HashAlgo_HASH_ALGO_BLAKE3, Value: []byte{0x01, 0x02, 0x03}},
					Size: 42,
				},
			},
		},
	}
	encoded, err := proto.Marshal(release)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current.release"), encoded, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "current.release.sig"), []byte("подпись"), 0o644); err != nil {
		t.Fatal(err)
	}
	return launchersvc.NewReleases(dir)
}

func downloadRequest(t *testing.T, service *Service, path, agent string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	if agent != "" {
		request.Header.Set("User-Agent", agent)
	}
	recorder := httptest.NewRecorder()
	service.DownloadHandler().ServeHTTP(recorder, request)
	return recorder
}

func TestDownloadPointsAtTheObjectWithItsName(t *testing.T) {
	service := NewService(Options{Releases: publishedLauncher(t)})

	got := downloadRequest(t, service, "/launcher/windows-x64", "")
	if got.Code != http.StatusFound {
		t.Fatalf("ждали перенаправление, получили %d", got.Code)
	}
	location := got.Header().Get("Location")
	if !strings.HasPrefix(location, "/objects/blake3/") {
		t.Fatalf("ссылка не ведёт на объект: %s", location)
	}
	if !strings.Contains(location, "filename=MagicWorld.exe") {
		t.Fatalf("имя файла потерялось: %s", location)
	}
}

func TestDownloadPicksSystemByBrowser(t *testing.T) {
	service := NewService(Options{Releases: publishedLauncher(t)})

	windows := downloadRequest(t, service, "/launcher", "Mozilla/5.0 (Windows NT 10.0; Win64; x64)")
	if windows.Header().Get("Location") != "/launcher/windows-x64" {
		t.Fatalf("Windows не распознан: %s", windows.Header().Get("Location"))
	}
	linux := downloadRequest(t, service, "/launcher", "Mozilla/5.0 (X11; Linux x86_64)")
	if linux.Header().Get("Location") != "/launcher/linux" {
		t.Fatalf("Linux не распознан: %s", linux.Header().Get("Location"))
	}
}

func TestDownloadListsSystemsWhenUnknown(t *testing.T) {
	service := NewService(Options{Releases: publishedLauncher(t)})

	got := downloadRequest(t, service, "/launcher", "curl/8.0")
	if got.Code != http.StatusOK {
		t.Fatalf("ждали список, получили %d", got.Code)
	}
	body := got.Body.String()
	for _, want := range []string{"/launcher/windows-x64", "/launcher/linux", "1.0.0"} {
		if !strings.Contains(body, want) {
			t.Fatalf("в списке нет %q: %s", want, body)
		}
	}
}

func TestDownloadWithoutPublishedLauncher(t *testing.T) {
	service := NewService(Options{Releases: launchersvc.NewReleases(t.TempDir())})

	if got := downloadRequest(t, service, "/launcher/windows-x64", ""); got.Code != http.StatusNotFound {
		t.Fatalf("без опубликованного лаунчера ждали 404, получили %d", got.Code)
	}
}

func TestDownloadUnknownPlatform(t *testing.T) {
	service := NewService(Options{Releases: publishedLauncher(t)})

	if got := downloadRequest(t, service, "/launcher/plan9", ""); got.Code != http.StatusNotFound {
		t.Fatalf("для несуществующей системы ждали 404, получили %d", got.Code)
	}
	if got := downloadRequest(t, service, "/launcher/mac-os", ""); got.Code != http.StatusNotFound {
		t.Fatalf("для неопубликованной системы ждали 404, получили %d", got.Code)
	}
}

func TestDownloadedNameIsSafe(t *testing.T) {
	for _, name := range []string{"../../etc/passwd", "плохое\"имя", "a\nb"} {
		cleaned := sanitiseName(name)
		if strings.ContainsAny(cleaned, `/\"`) || strings.ContainsRune(cleaned, '\n') {
			t.Fatalf("имя не вычищено: %q -> %q", name, cleaned)
		}
	}
}
