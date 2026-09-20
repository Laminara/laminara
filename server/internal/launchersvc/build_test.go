package launchersvc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/bake"
	"github.com/laminara/laminara/server/internal/clientconfig"
	"github.com/laminara/laminara/server/internal/macapp"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/storage"
)

func fakeReleases(t *testing.T, tag string, templates map[string][]byte) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	var checksums strings.Builder
	for name, body := range templates {
		digest := sha256.Sum256(body)
		fmt.Fprintf(&checksums, "%s  %s\n", hex.EncodeToString(digest[:]), name)
		mux.HandleFunc("/assets/"+name, func(w http.ResponseWriter, _ *http.Request) {
			w.Write(body)
		})
	}
	mux.HandleFunc("/assets/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(checksums.String()))
	})
	mux.HandleFunc("/repos/owner/repo/releases/tags/"+tag, func(w http.ResponseWriter, _ *http.Request) {
		assets := []map[string]string{{"name": "checksums.txt", "browser_download_url": server.URL + "/assets/checksums.txt"}}
		for name := range templates {
			assets = append(assets, map[string]string{"name": name, "browser_download_url": server.URL + "/assets/" + name})
		}
		json.NewEncoder(w).Encode(map[string]any{"tag_name": tag, "assets": assets})
	})
	return server
}

func bakingService(t *testing.T, releases *httptest.Server, document clientconfig.Document) (*Service, string) {
	t.Helper()
	dir := t.TempDir()
	config, _ := json.Marshal(map[string]string{"root": t.TempDir()})
	backend, err := storage.BuildBackend("fs", config)
	if err != nil {
		t.Fatal(err)
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(storage.NewCAS(backend, corev1.HashAlgo_HASH_ALGO_BLAKE3), manifest.NewSigner(priv), dir)
	service.SetBakery(&Bakery{
		Repo:     "owner/repo",
		Version:  "1.2.3",
		API:      releases.URL,
		HTTP:     releases.Client(),
		Document: func() (clientconfig.Document, error) { return document, nil },
	})
	return service, dir
}

func demoDocument() clientconfig.Document {
	return clientconfig.Document{
		Endpoints:          []clientconfig.Endpoint{{ID: "play", BaseURL: "https://play.example"}},
		ServerPublicKeyHex: "abc123",
		Branding:           &clientconfig.Branding{Name: "Пример", WindowTitle: "Пример"},
	}
}

func TestBuildBakesTheConfigAndPublishes(t *testing.T) {
	templates := map[string][]byte{
		linuxTemplate:   []byte("linux launcher template"),
		windowsTemplate: []byte("windows launcher template"),
	}
	releases := fakeReleases(t, "v1.2.3", templates)
	service, dir := bakingService(t, releases, demoDocument())

	var out bytes.Buffer
	if err := service.build(context.Background(), nil, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	for name, template := range map[string][]byte{
		"Пример":     templates[linuxTemplate],
		"Пример.exe": templates[windowsTemplate],
	} {
		image, err := os.ReadFile(filepath.Join(dir, "1.2.3", name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(bake.Strip(image), template) {
			t.Fatalf("%s does not start with its template", name)
		}
		carried, ok := bake.Read(image)
		if !ok {
			t.Fatalf("%s carries no config", name)
		}
		if !bytes.Contains(carried, []byte("https://play.example")) {
			t.Fatalf("%s carries the wrong config: %s", name, carried)
		}
	}

	release, _, err := NewReleases(dir).Current()
	if err != nil || len(release) == 0 {
		t.Fatalf("build must publish what it baked: %v", err)
	}
	decoded, err := Decode(release)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Version != "1.2.3" || len(decoded.Artifacts) != 2 {
		t.Fatalf("published release = %+v", decoded)
	}
}

func TestBuildWrapsMacOSIntoAnAppBundle(t *testing.T) {
	templates := map[string][]byte{
		linuxTemplate:   []byte("linux launcher template"),
		windowsTemplate: []byte("windows launcher template"),
		macTemplate:     []byte("universal mach-o, signed by apple's own tooling"),
	}
	releases := fakeReleases(t, "v1.2.3", templates)
	service, dir := bakingService(t, releases, demoDocument())

	var out bytes.Buffer
	if err := service.build(context.Background(), nil, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}

	archive, err := os.ReadFile(filepath.Join(dir, "1.2.3", "Пример-mac-os-arm64.app.tar.gz"))
	if err != nil {
		t.Fatalf("пакет для Apple Silicon не собрался: %v\n%s", err, out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "1.2.3", "Пример-mac-os.app.tar.gz")); err != nil {
		t.Fatalf("пакет для маков на Intel не собрался: %v", err)
	}

	entries := map[string][]byte{}
	unzipped, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	bodies := tar.NewReader(unzipped)
	for {
		header, err := bodies.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(bodies)
		if err != nil {
			t.Fatal(err)
		}
		entries[header.Name] = body
	}

	if !bytes.Equal(entries["Пример.app/Contents/MacOS/laminara"], templates[macTemplate]) {
		t.Fatal("исполняемый файл тронули — на Apple Silicon такой лаунчер система не запустит")
	}
	if !bytes.Contains(entries["Пример.app/Contents/Resources/"+macapp.ConfigName], []byte("https://play.example")) {
		t.Fatalf("настройки не легли в пакет: %s", entries["Пример.app/Contents/Resources/"+macapp.ConfigName])
	}

	release, _, err := NewReleases(dir).Current()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(release)
	if err != nil {
		t.Fatal(err)
	}
	wanted := map[corev1.Platform]bool{
		corev1.Platform_PLATFORM_MAC_OS:       false,
		corev1.Platform_PLATFORM_MAC_OS_ARM64: false,
	}
	for _, artifact := range decoded.Artifacts {
		if _, ok := wanted[artifact.Platform]; !ok {
			continue
		}
		if artifact.Kind != corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_BUNDLE_TAR_GZ {
			t.Fatalf("%v опубликован как %v", artifact.Platform, artifact.Kind)
		}
		wanted[artifact.Platform] = true
	}
	for target, published := range wanted {
		if !published {
			t.Fatalf("для %v лаунчер не опубликован — обновляться будет нечем", target)
		}
	}
}

func TestBuildSkipsMacOSWhenTheReleaseHasNoBinary(t *testing.T) {
	templates := map[string][]byte{
		linuxTemplate:   []byte("linux launcher template"),
		windowsTemplate: []byte("windows launcher template"),
	}
	releases := fakeReleases(t, "v1.2.3", templates)
	service, dir := bakingService(t, releases, demoDocument())

	var out bytes.Buffer
	if err := service.build(context.Background(), nil, &out); err != nil {
		t.Fatalf("старый релиз без macOS обязан собираться дальше: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), macTemplate) {
		t.Fatalf("пропуск macOS должен быть виден оператору:\n%s", out.String())
	}
	entries, err := os.ReadDir(filepath.Join(dir, "1.2.3"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), ".app.tar.gz") {
			t.Fatalf("появился пакет для macOS, хотя собирать его было не из чего: %s", entry.Name())
		}
	}
}

func TestBuildCachesTheTemplateItAlreadyDownloaded(t *testing.T) {
	templates := map[string][]byte{
		linuxTemplate:   []byte("linux launcher template"),
		windowsTemplate: []byte("windows launcher template"),
	}
	releases := fakeReleases(t, "v1.2.3", templates)
	service, dir := bakingService(t, releases, demoDocument())

	var out bytes.Buffer
	if err := service.build(context.Background(), nil, &out); err != nil {
		t.Fatal(err)
	}
	releases.Close()

	document := demoDocument()
	document.Endpoints[0].BaseURL = "https://second.example"
	service.SetBakery(&Bakery{
		Repo:     "owner/repo",
		Version:  "1.2.4",
		API:      releases.URL,
		HTTP:     releases.Client(),
		Document: func() (clientconfig.Document, error) { return document, nil },
	})
	if err := os.MkdirAll(filepath.Join(dir, templatesDir, "1.2.4"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range templates {
		if err := os.WriteFile(filepath.Join(dir, templatesDir, "1.2.4", name), body, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if err := service.build(context.Background(), nil, &out); err != nil {
		t.Fatalf("a cached template must be reused instead of downloaded again: %v", err)
	}
	image, err := os.ReadFile(filepath.Join(dir, "1.2.4", "Пример"))
	if err != nil {
		t.Fatal(err)
	}
	carried, _ := bake.Read(image)
	if !bytes.Contains(carried, []byte("https://second.example")) {
		t.Fatalf("rebuild carried the old config: %s", carried)
	}
}

func TestBuildRefusesToRepublishTheSameVersion(t *testing.T) {
	templates := map[string][]byte{
		linuxTemplate:   []byte("linux launcher template"),
		windowsTemplate: []byte("windows launcher template"),
	}
	releases := fakeReleases(t, "v1.2.3", templates)
	service, _ := bakingService(t, releases, demoDocument())

	var out bytes.Buffer
	if err := service.build(context.Background(), nil, &out); err != nil {
		t.Fatal(err)
	}
	err := service.build(context.Background(), nil, &out)
	if err == nil || !strings.Contains(err.Error(), "1.2.4") {
		t.Fatalf("a second build of the same version must point at the next one, got %v", err)
	}
}

func TestBuildNeedsAnEndpoint(t *testing.T) {
	releases := fakeReleases(t, "v1.2.3", map[string][]byte{linuxTemplate: []byte("x")})
	document := demoDocument()
	document.Endpoints = nil
	service, _ := bakingService(t, releases, document)

	var out bytes.Buffer
	err := service.build(context.Background(), nil, &out)
	if err == nil || !strings.Contains(err.Error(), "launcher.endpoints") {
		t.Fatalf("without an endpoint the build must say so, got %v", err)
	}
}

func TestBakedLauncherStarts(t *testing.T) {
	template := os.Getenv("LAMINARA_LAUNCHER_TEMPLATE")
	if template == "" {
		t.Skip("нет LAMINARA_LAUNCHER_TEMPLATE — тесту нужен собранный шаблон лаунчера")
	}
	body, err := os.ReadFile(template)
	if err != nil {
		t.Fatal(err)
	}
	document := demoDocument()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	document.ServerPublicKeyHex = hex.EncodeToString(pub)
	payload, err := document.JSON()
	if err != nil {
		t.Fatal(err)
	}
	image, err := bake.Attach(body, payload)
	if err != nil {
		t.Fatal(err)
	}
	var log bytes.Buffer
	image = brand(image, document, &log)

	path := filepath.Join(t.TempDir(), "launcher"+filepath.Ext(template))
	if err := os.WriteFile(path, image, 0o755); err != nil {
		t.Fatal(err)
	}

	command := exec.Command(path)
	if err := command.Start(); err != nil {
		t.Fatalf("запечённый лаунчер не запускается: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err := <-done:
		t.Fatalf("запечённый лаунчер сразу завершился: %v\n%s", err, log.String())
	case <-time.After(8 * time.Second):
		_ = command.Process.Kill()
	}
}

func TestOperatorVersionDoesNotChooseTheTemplateRelease(t *testing.T) {
	templates := map[string][]byte{
		linuxTemplate:   []byte("linux launcher template"),
		windowsTemplate: []byte("windows launcher template"),
	}
	releases := fakeReleases(t, "v1.2.3", templates)
	service, dir := bakingService(t, releases, demoDocument())

	var first bytes.Buffer
	if err := service.build(context.Background(), nil, &first); err != nil {
		t.Fatalf("первая сборка: %v\n%s", err, first.String())
	}

	var second bytes.Buffer
	if err := service.build(context.Background(), []string{"9.9.9"}, &second); err != nil {
		t.Fatalf("сборку своей версии не дали выпустить: %v\n%s", err, second.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "9.9.9")); err != nil {
		t.Fatalf("версия оператора не собралась: %v", err)
	}
}
