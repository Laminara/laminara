package macapp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func unpack(t *testing.T, archive []byte) map[string]tar.Header {
	t.Helper()
	reader, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]tar.Header{}
	bodies := tar.NewReader(reader)
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
		header.Linkname = string(body)
		entries[header.Name] = *header
	}
	return entries
}

func demoBundle() Bundle {
	return Bundle{
		Name:       "Пример",
		Version:    "1.13.0",
		Executable: []byte("universal mach-o"),
		Config:     []byte(`{"endpoints":[{"id":"play","baseUrl":"https://play.example"}]}`),
		Icon:       []byte("icns"),
	}
}

func TestBundleCarriesEverythingMacOSNeeds(t *testing.T) {
	archive, err := demoBundle().TarGz()
	if err != nil {
		t.Fatal(err)
	}
	entries := unpack(t, archive)

	executable, ok := entries["Пример.app/Contents/MacOS/laminara"]
	if !ok {
		t.Fatalf("в пакете нет исполняемого файла: %v", entries)
	}
	if executable.Mode&0o111 == 0 {
		t.Fatalf("файл в Contents/MacOS обязан быть исполняемым: %o", executable.Mode)
	}
	if executable.Linkname != "universal mach-o" {
		t.Fatal("исполняемый файл в пакете изменён — подпись Apple такого не переживёт")
	}

	config, ok := entries["Пример.app/Contents/Resources/"+ConfigName]
	if !ok || !strings.Contains(config.Linkname, "https://play.example") {
		t.Fatalf("настройки лаунчера не легли рядом с приложением: %+v", config)
	}
	if _, ok := entries["Пример.app/Contents/Resources/"+IconName]; !ok {
		t.Fatal("иконка не попала в пакет")
	}

	plist, ok := entries["Пример.app/Contents/Info.plist"]
	if !ok {
		t.Fatal("без Info.plist macOS не считает папку приложением")
	}
	for _, want := range []string{
		"<key>CFBundleName</key>\n\t<string>Пример</string>",
		"<key>CFBundleExecutable</key>\n\t<string>laminara</string>",
		"<key>CFBundleIconFile</key>\n\t<string>icon</string>",
		"<string>1.13.0</string>",
		"<key>NSMicrophoneUsageDescription</key>",
		"<key>NSLocalNetworkUsageDescription</key>",
	} {
		if !strings.Contains(plist.Linkname, want) {
			t.Fatalf("в Info.plist нет %q:\n%s", want, plist.Linkname)
		}
	}
}

func TestBundleWithoutAnIconDoesNotPromiseOne(t *testing.T) {
	bundle := demoBundle()
	bundle.Icon = nil
	archive, err := bundle.TarGz()
	if err != nil {
		t.Fatal(err)
	}
	entries := unpack(t, archive)
	if _, ok := entries["Пример.app/Contents/Resources/"+IconName]; ok {
		t.Fatal("иконки нет, а файл появился")
	}
	if strings.Contains(entries["Пример.app/Contents/Info.plist"].Linkname, "CFBundleIconFile") {
		t.Fatal("Info.plist обещает иконку, которой нет")
	}
}

func TestBundleNeedsSomethingToRun(t *testing.T) {
	bundle := demoBundle()
	bundle.Executable = nil
	if _, err := bundle.TarGz(); err == nil {
		t.Fatal("пакет без исполняемого файла собирать нельзя")
	}
}

func TestBundleNameSurvivesAwkwardBranding(t *testing.T) {
	for name, want := range map[string]string{
		"Пример":      "Пример.app",
		"Laminara":    "Laminara.app",
		"Есть/слэш":   "Естьслэш.app",
		"  ":          "Laminara.app",
		"Готово.app":  "Готово.app",
		"Two  Words ": "Two  Words.app",
	} {
		if got := BundleName(name); got != want {
			t.Fatalf("BundleName(%q) = %q, ждали %q", name, got, want)
		}
	}
}

func TestIdentifierStaysUniquePerLauncher(t *testing.T) {
	if got := Identifier("Laminara Launcher"); got != "dev.laminara.laminara-launcher" {
		t.Fatalf("латиница обязана читаться в имени: %s", got)
	}
	first, second := Identifier("Пример"), Identifier("Стенд")
	if first == second {
		t.Fatalf("два лаунчера получили один и тот же CFBundleIdentifier: %s", first)
	}
	if !strings.HasPrefix(first, "dev.laminara.") {
		t.Fatalf("идентификатор не похож на обратный домен: %s", first)
	}
}
