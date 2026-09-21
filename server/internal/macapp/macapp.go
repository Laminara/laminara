package macapp

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	ConfigName     = "laminara.client.json"
	IconName       = "icon.icns"
	ExecutableName = "laminara"
	minimumSystem  = "10.15"

	microphoneReason   = "Микрофон нужен модам голосового чата — игра спрашивает его через лаунчер."
	localNetworkReason = "Доступ к локальной сети нужен, чтобы видеть игровые серверы и миры по локальной сети."
)

var ErrNoExecutable = errors.New("в пакете для macOS нечего запускать")

type Bundle struct {
	Name       string
	Version    string
	Executable []byte
	Icon       []byte
	Config     []byte
}

type bundleFile struct {
	name string
	mode int64
	body []byte
}

type plistEntry struct {
	key   string
	value string
}

func (b Bundle) files() ([]bundleFile, error) {
	if len(b.Executable) == 0 {
		return nil, ErrNoExecutable
	}
	plist, err := b.infoPlist()
	if err != nil {
		return nil, err
	}
	root := BundleName(b.Name)
	wanted := []bundleFile{
		{root + "/Contents/Info.plist", 0o644, plist},
		{root + "/Contents/MacOS/" + ExecutableName, 0o755, b.Executable},
		{root + "/Contents/Resources/" + ConfigName, 0o644, b.Config},
		{root + "/Contents/Resources/" + IconName, 0o644, b.Icon},
	}
	files := make([]bundleFile, 0, len(wanted))
	for _, file := range wanted {
		if len(file.body) > 0 {
			files = append(files, file)
		}
	}
	return files, nil
}

func (b Bundle) Write(dir string) (string, error) {
	files, err := b.files()
	if err != nil {
		return "", err
	}
	root := filepath.Join(dir, BundleName(b.Name))
	if err := os.RemoveAll(root); err != nil {
		return "", err
	}
	for _, file := range files {
		path := filepath.Join(dir, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, file.body, os.FileMode(file.mode)); err != nil {
			return "", err
		}
	}
	return root, nil
}

func (b Bundle) TarGz() ([]byte, error) {
	files, err := b.files()
	if err != nil {
		return nil, err
	}
	root := BundleName(b.Name)

	var body bytes.Buffer
	compressed := gzip.NewWriter(&body)
	archive := tar.NewWriter(compressed)
	stamp := time.Unix(0, 0).UTC()

	for _, dir := range []string{root, root + "/Contents", root + "/Contents/MacOS", root + "/Contents/Resources"} {
		if err := archive.WriteHeader(&tar.Header{
			Name:     dir + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
			ModTime:  stamp,
		}); err != nil {
			return nil, err
		}
	}

	for _, file := range files {
		if err := archive.WriteHeader(&tar.Header{
			Name:    file.name,
			Mode:    file.mode,
			Size:    int64(len(file.body)),
			ModTime: stamp,
		}); err != nil {
			return nil, err
		}
		if _, err := archive.Write(file.body); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := compressed.Close(); err != nil {
		return nil, err
	}
	return body.Bytes(), nil
}

func BundleName(name string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(name), ".app")
	trimmed = strings.Map(func(r rune) rune {
		if r == '/' || r == ':' || r < 0x20 {
			return -1
		}
		return r
	}, trimmed)
	if trimmed == "" {
		trimmed = "Laminara"
	}
	return trimmed + ".app"
}

func Identifier(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out.WriteRune(r)
		case r == '-' || r == ' ' || r == '_':
			out.WriteRune('-')
		}
	}
	slug := strings.Trim(out.String(), "-")
	for strings.Contains(slug, "--") {
		slug = strings.ReplaceAll(slug, "--", "-")
	}
	if slug == "" {
		digest := sha256.Sum256([]byte(name))
		slug = hex.EncodeToString(digest[:5])
	}
	return "dev.laminara." + slug
}

func (b Bundle) infoPlist() ([]byte, error) {
	name := strings.TrimSuffix(BundleName(b.Name), ".app")
	version := strings.TrimSpace(b.Version)
	if version == "" {
		version = "0.0.0"
	}
	entries := []plistEntry{
		{"CFBundleDevelopmentRegion", "en"},
		{"CFBundleDisplayName", name},
		{"CFBundleExecutable", ExecutableName},
		{"CFBundleIdentifier", Identifier(b.Name)},
		{"CFBundleInfoDictionaryVersion", "6.0"},
		{"CFBundleName", name},
		{"CFBundlePackageType", "APPL"},
		{"CFBundleShortVersionString", version},
		{"CFBundleVersion", version},
		{"LSMinimumSystemVersion", minimumSystem},
		{"NSMicrophoneUsageDescription", microphoneReason},
		{"NSLocalNetworkUsageDescription", localNetworkReason},
	}
	if len(b.Icon) > 0 {
		entries = append(entries, plistEntry{"CFBundleIconFile", strings.TrimSuffix(IconName, ".icns")})
	}

	var out bytes.Buffer
	out.WriteString(xml.Header)
	out.WriteString("<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n")
	out.WriteString("<plist version=\"1.0\">\n<dict>\n")
	for _, entry := range entries {
		key, err := escaped(entry.key)
		if err != nil {
			return nil, err
		}
		value, err := escaped(entry.value)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&out, "\t<key>%s</key>\n\t<string>%s</string>\n", key, value)
	}
	out.WriteString("\t<key>NSHighResolutionCapable</key>\n\t<true/>\n")
	out.WriteString("\t<key>NSSupportsAutomaticGraphicsSwitching</key>\n\t<true/>\n")
	out.WriteString("\t<key>NSAppTransportSecurity</key>\n\t<dict>\n\t\t<key>NSAllowsArbitraryLoads</key>\n\t\t<true/>\n\t</dict>\n")
	out.WriteString("</dict>\n</plist>\n")
	return out.Bytes(), nil
}

func escaped(value string) (string, error) {
	var out bytes.Buffer
	if err := xml.EscapeText(&out, []byte(value)); err != nil {
		return "", err
	}
	return out.String(), nil
}
