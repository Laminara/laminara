package launchersvc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/platform"
	"github.com/laminara/laminara/server/internal/storage"
)

const (
	releaseFile   = "current.release"
	signatureFile = "current.release.sig"
	schemaVersion = 1
)

type Service struct {
	cas    *storage.CAS
	signer *manifest.Signer
	dir    string
	emit   func(topic string, data map[string]string)
}

func NewService(cas *storage.CAS, signer *manifest.Signer, dir string) *Service {
	return &Service{cas: cas, signer: signer, dir: dir}
}

func (s *Service) SetEmitter(emit func(topic string, data map[string]string)) {
	s.emit = emit
}

func (s *Service) Commands() []command.Command {
	return []command.Command{{
		Name:     "launcher",
		Synopsis: "версии лаунчера (launcher versions | launcher publish <версия> | launcher status)",
		Run:      s.run,
	}}
}

func (s *Service) run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("как пользоваться: launcher versions | launcher publish <версия> | launcher status")
	}
	switch args[0] {
	case "versions":
		return s.versions(out)
	case "publish":
		if len(args) < 2 {
			return fmt.Errorf("как пользоваться: launcher publish <версия>")
		}
		return s.publish(ctx, args[1], out)
	case "status":
		return s.status(out)
	default:
		return fmt.Errorf("не знаю подкоманду launcher %q", args[0])
	}
}

func (s *Service) versions(out io.Writer) error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintf(out, "Папки с лаунчерами пока нет: %s\n", s.dir)
			return nil
		}
		return err
	}
	found := false
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		artifacts, _ := os.ReadDir(filepath.Join(s.dir, entry.Name()))
		fmt.Fprintf(out, "%-12s %s\n", entry.Name(), humanize.Count(len(artifacts), "файл", "файла", "файлов"))
		found = true
	}
	if !found {
		fmt.Fprintf(out, "В %s пока пусто — создайте папку %s/<версия>/ и положите туда собранные лаунчеры\n", s.dir, s.dir)
	}
	return nil
}

func (s *Service) status(out io.Writer) error {
	release, _, err := NewReleases(s.dir).Current()
	if err != nil {
		return err
	}
	if len(release) == 0 {
		fmt.Fprintln(out, "Ни одна версия лаунчера не опубликована.")
		return nil
	}
	decoded, err := Decode(release)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Опубликована версия %s, файлов: %d\n", decoded.Version, len(decoded.Artifacts))
	for _, artifact := range decoded.Artifacts {
		key, _ := platform.Key(artifact.Platform)
		fmt.Fprintf(out, "  %-14s %-24s %s\n", key, kindWord(artifact.Kind), artifact.FileName)
	}
	return nil
}

func (s *Service) publish(ctx context.Context, version string, out io.Writer) error {
	version = strings.TrimSpace(strings.TrimPrefix(version, "v"))
	if !validVersion(version) {
		return fmt.Errorf("версия %q не похожа на номер вида 1.2.3", version)
	}
	current, _, err := NewReleases(s.dir).Current()
	if err != nil {
		return err
	}
	if len(current) > 0 {
		decoded, err := Decode(current)
		if err != nil {
			return err
		}
		if compareVersions(version, decoded.Version) <= 0 {
			return fmt.Errorf("версия %s должна быть больше уже опубликованной %s", version, decoded.Version)
		}
	}

	dir := filepath.Join(s.dir, version)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("не читается папка %s: %w", dir, err)
	}

	release := &corev1.LauncherRelease{
		SchemaVersion:       schemaVersion,
		Version:             version,
		ReleasedAtUnixNanos: time.Now().UnixNano(),
	}
	seen := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		target, kind, ok := classify(entry.Name())
		if !ok {
			fmt.Fprintf(out, "Пропускаю %s — не понимаю, для какой системы этот файл\n", entry.Name())
			continue
		}
		key, _ := platform.Key(target)
		slot := key + "/" + kindName(kind)
		if seen[slot] {
			return fmt.Errorf("на место %s претендуют два файла", slot)
		}
		seen[slot] = true

		file, err := os.Open(filepath.Join(dir, entry.Name()))
		if err != nil {
			return err
		}
		ref, err := s.cas.Put(ctx, file)
		file.Close()
		if err != nil {
			return err
		}
		release.Artifacts = append(release.Artifacts, &corev1.LauncherArtifact{
			Platform: target,
			Kind:     kind,
			Object:   ref,
			FileName: entry.Name(),
		})
		fmt.Fprintf(out, "  %-14s %-22s %s (%s)\n", key, kindName(kind), entry.Name(), humanize.Bytes(uint64(ref.Size)))
	}
	if len(release.Artifacts) == 0 {
		return fmt.Errorf("в папке %s нет ни одного понятного файла лаунчера", dir)
	}
	sort.Slice(release.Artifacts, func(i, j int) bool {
		if release.Artifacts[i].Platform != release.Artifacts[j].Platform {
			return release.Artifacts[i].Platform < release.Artifacts[j].Platform
		}
		return release.Artifacts[i].Kind < release.Artifacts[j].Kind
	})

	canonical, signature, err := s.signer.SignMessage(release)
	if err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.dir, signatureFile), signature); err != nil {
		return err
	}
	if err := writeAtomic(filepath.Join(s.dir, releaseFile), canonical); err != nil {
		return err
	}
	if s.emit != nil {
		s.emit("launcher.published", map[string]string{"version": version})
	}
	fmt.Fprintf(out, "Лаунчер %s опубликован — старые версии обновятся сами\n", version)
	return nil
}

func writeAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func classify(name string) (corev1.Platform, corev1.LauncherArtifactKind, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".appimage"):
		return platformFromName(lower, corev1.Platform_PLATFORM_LINUX), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_IMAGE, true
	case strings.HasSuffix(lower, ".app.tar.gz"):
		return platformFromName(lower, corev1.Platform_PLATFORM_MAC_OS), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_BUNDLE_TAR_GZ, true
	case strings.HasSuffix(lower, ".exe"):
		if strings.Contains(lower, "setup") || strings.Contains(lower, "installer") {
			return platformFromName(lower, corev1.Platform_PLATFORM_WINDOWS_X64), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER, true
		}
		return platformFromName(lower, corev1.Platform_PLATFORM_WINDOWS_X64), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE, true
	case strings.HasSuffix(lower, ".msi"):
		return platformFromName(lower, corev1.Platform_PLATFORM_WINDOWS_X64), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER, true
	case strings.HasSuffix(lower, ".dmg"), strings.HasSuffix(lower, ".pkg"):
		return platformFromName(lower, corev1.Platform_PLATFORM_MAC_OS), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER, true
	case strings.HasSuffix(lower, ".deb"), strings.HasSuffix(lower, ".rpm"):
		return platformFromName(lower, corev1.Platform_PLATFORM_LINUX), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER, true
	case strings.Contains(lower, "mac-os") || strings.Contains(lower, "macos") || strings.Contains(lower, "darwin"):
		return platformFromName(lower, corev1.Platform_PLATFORM_MAC_OS), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE, true
	case strings.Contains(lower, "linux"), filepath.Ext(lower) == "":
		return platformFromName(lower, corev1.Platform_PLATFORM_LINUX), corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE, true
	default:
		return corev1.Platform_PLATFORM_UNSPECIFIED, corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_UNSPECIFIED, false
	}
}

func platformFromName(lower string, fallback corev1.Platform) corev1.Platform {
	for _, p := range []corev1.Platform{
		corev1.Platform_PLATFORM_WINDOWS_ARM64,
		corev1.Platform_PLATFORM_WINDOWS_X86,
		corev1.Platform_PLATFORM_WINDOWS_X64,
		corev1.Platform_PLATFORM_MAC_OS_ARM64,
		corev1.Platform_PLATFORM_MAC_OS,
		corev1.Platform_PLATFORM_LINUX_ARM64,
		corev1.Platform_PLATFORM_LINUX_I386,
		corev1.Platform_PLATFORM_LINUX,
	} {
		if key, ok := platform.Key(p); ok && strings.Contains(lower, key) {
			return p
		}
	}
	if strings.Contains(lower, "aarch64") || strings.Contains(lower, "arm64") {
		switch fallback {
		case corev1.Platform_PLATFORM_MAC_OS:
			return corev1.Platform_PLATFORM_MAC_OS_ARM64
		case corev1.Platform_PLATFORM_LINUX:
			return corev1.Platform_PLATFORM_LINUX_ARM64
		case corev1.Platform_PLATFORM_WINDOWS_X64:
			return corev1.Platform_PLATFORM_WINDOWS_ARM64
		}
	}
	return fallback
}

func kindName(kind corev1.LauncherArtifactKind) string {
	name := corev1.LauncherArtifactKind_name[int32(kind)]
	return strings.ToLower(strings.TrimPrefix(name, "LAUNCHER_ARTIFACT_KIND_"))
}

func validVersion(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		if _, err := strconv.Atoi(strings.SplitN(part, "-", 2)[0]); err != nil {
			return false
		}
	}
	return true
}

func compareVersions(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		av, bv := 0, 0
		if i < len(as) {
			av, _ = strconv.Atoi(strings.SplitN(as[i], "-", 2)[0])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(strings.SplitN(bs[i], "-", 2)[0])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

func kindWord(kind corev1.LauncherArtifactKind) string {
	switch kind {
	case corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_RAW_EXECUTABLE:
		return "готовый файл"
	case corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_INSTALLER:
		return "установщик"
	case corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_IMAGE:
		return "AppImage"
	case corev1.LauncherArtifactKind_LAUNCHER_ARTIFACT_KIND_APP_BUNDLE_TAR_GZ:
		return "пакет для macOS"
	default:
		return kindName(kind)
	}
}
