package buildsvc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/platform"
	"github.com/laminara/laminara/server/internal/prepare"
	"github.com/laminara/laminara/server/internal/storage"
)

type Service struct {
	mojang      *mojang.Client
	preparer    *prepare.Preparer
	cas         *storage.CAS
	signer      *manifest.Signer
	profilesDir string
	emit        func(topic string, data map[string]string)
}

func NewService(cas *storage.CAS, signer *manifest.Signer, profilesDir string) *Service {
	return &Service{
		mojang:      mojang.NewClient(),
		preparer:    prepare.NewPreparer(),
		cas:         cas,
		signer:      signer,
		profilesDir: profilesDir,
	}
}

func (s *Service) SetEmitter(emit func(topic string, data map[string]string)) {
	s.emit = emit
}

func (s *Service) fire(topic, name string) {
	if s.emit != nil {
		s.emit(topic, map[string]string{"name": name})
	}
}

var platforms = map[string][2]string{
	"windows-x64":   {"windows", "x86_64"},
	"windows-x86":   {"windows", "x86"},
	"windows-arm64": {"windows", "arm64"},
	"linux":         {"linux", "x86_64"},
	"linux-i386":    {"linux", "x86"},
	"mac-os":        {"osx", "x86_64"},
	"mac-os-arm64":  {"osx", "arm64"},
}

const defaultPlatform = "windows-x64"

func (s *Service) Commands() []command.Command {
	return []command.Command{
		{Name: "versions", Synopsis: "версии Minecraft (versions [фильтр])", Run: s.versions},
		{Name: "loaders", Synopsis: "загрузчики модов для версии (loaders <версия>)", Run: s.loaders},
		{Name: "install", Aliases: []string{"prepare"}, Synopsis: "собрать клиент (install <имя> <версия> [loader=..] [loaderVersion=..] [platform=..] [java=..])", Run: s.prepare},
		{Name: "publish", Aliases: []string{"release"}, Synopsis: "опубликовать сборку — лаунчеры увидят её (publish <имя>)", Run: s.publish},
		{Name: "builds", Aliases: []string{"clients"}, Synopsis: "сборки на сервере и их состояние", Run: s.builds},
		{Name: "delete", Aliases: []string{"deletebuild", "remove"}, Synopsis: "удалить сборку (delete <имя>)", Run: s.delete},
	}
}

func safeName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return false
	}
	for _, p := range platform.Game() {
		if key, ok := platform.Key(p); ok && strings.HasSuffix(name, "."+key) {
			return false
		}
	}
	return true
}

func (s *Service) builds(_ context.Context, _ []string, out io.Writer) error {
	builds, err := s.Builds()
	if err != nil {
		return err
	}
	if len(builds) == 0 {
		fmt.Fprintln(out, "Сборок пока нет.")
		return nil
	}
	for _, build := range builds {
		fmt.Fprintf(out, "%-20s %s\n", build.Name, statusWord(build.Status))
	}
	return nil
}

func (s *Service) manifestsOf(name string) []string {
	var found []string
	flat := filepath.Join(s.profilesDir, name+".manifest")
	if _, err := os.Stat(flat); err == nil {
		found = append(found, flat)
	}
	for _, p := range platform.Game() {
		key, ok := platform.Key(p)
		if !ok {
			continue
		}
		candidate := filepath.Join(s.profilesDir, name+"."+key+".manifest")
		if _, err := os.Stat(candidate); err == nil {
			found = append(found, candidate)
		}
	}
	return found
}

func (s *Service) Builds() ([]admin.BuildEntry, error) {
	entries, err := os.ReadDir(s.profilesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var builds []admin.BuildEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		status := "prepared"
		if len(s.manifestsOf(entry.Name())) > 0 {
			status = "published"
		}
		builds = append(builds, admin.BuildEntry{Name: entry.Name(), Status: status})
	}
	return builds, nil
}

func (s *Service) delete(_ context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: delete <name>")
	}
	name := args[0]
	if !safeName(name) {
		return fmt.Errorf("invalid build name %q", name)
	}
	dir := filepath.Join(s.profilesDir, name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("no build named %q", name)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	for _, manifestPath := range s.manifestsOf(name) {
		_ = os.Remove(manifestPath)
		_ = os.Remove(manifestPath + ".sig")
	}
	s.fire("build.deleted", name)
	fmt.Fprintf(out, "Сборка «%s» удалена.\n", name)
	return nil
}

func (s *Service) versions(ctx context.Context, args []string, out io.Writer) error {
	query := ""
	if len(args) > 0 {
		query = args[0]
	}
	list, err := s.Versions(ctx, query)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "Последний релиз: %s   последний снапшот: %s\n", list.LatestRelease, list.LatestSnapshot)
	for i, version := range list.Versions {
		if i >= 50 {
			fmt.Fprintln(out, "…дальше обрезано — уточните запрос, например: versions 1.21")
			break
		}
		fmt.Fprintf(out, "%-18s %s\n", version.ID, versionWord(version.Type))
	}
	return nil
}

func (s *Service) Versions(ctx context.Context, query string) (admin.VersionList, error) {
	list, err := s.mojang.ListVersions(ctx)
	if err != nil {
		return admin.VersionList{}, err
	}
	result := admin.VersionList{LatestRelease: list.Latest.Release, LatestSnapshot: list.Latest.Snapshot}
	for _, version := range list.Versions {
		if query != "" && !strings.Contains(version.ID, query) {
			continue
		}
		result.Versions = append(result.Versions, admin.VersionEntry{ID: version.ID, Type: version.Type})
	}
	return result, nil
}

func (s *Service) loaders(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: loaders <mcVersion>")
	}
	loaders, err := s.Loaders(ctx, args[0])
	if err != nil {
		return err
	}
	for _, entry := range loaders {
		if len(entry.Versions) == 0 {
			fmt.Fprintln(out, entry.Name)
			continue
		}
		fmt.Fprintf(out, "%-10s последняя %s, всего %s\n", entry.Name, entry.Versions[0], humanize.Count(len(entry.Versions), "версия", "версии", "версий"))
	}
	return nil
}

func (s *Service) Loaders(ctx context.Context, mcVersion string) ([]admin.LoaderEntry, error) {
	loaders := []admin.LoaderEntry{{Name: "vanilla"}}
	for _, l := range loader.All() {
		versions, err := l.Versions(ctx, mcVersion)
		if err != nil || len(versions) == 0 {
			continue
		}
		loaders = append(loaders, admin.LoaderEntry{Name: l.Name(), Versions: versions})
	}
	return loaders, nil
}

func (s *Service) prepare(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: install <name> <mc> [loader=..] [loaderVersion=..] [platform=..] [java=..]")
	}
	name, mcVersion := args[0], args[1]
	if !safeName(name) {
		return fmt.Errorf("invalid build name %q", name)
	}
	opts := parseKV(args[2:])

	targets, err := parsePlatforms(opts["platform"])
	if err != nil {
		return err
	}

	versionURL, id, err := s.versionURL(ctx, mcVersion)
	if err != nil {
		return err
	}

	loaderName := opts["loader"]
	loaderVersion := opts["loaderVersion"]
	if loaderName != "" && loaderName != "vanilla" && loaderVersion == "" {
		l, ok := loader.Get(loaderName)
		if !ok {
			return fmt.Errorf("unknown loader %q", loaderName)
		}
		versions, err := l.Versions(ctx, id)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return fmt.Errorf("loader %q has no versions for %s", loaderName, id)
		}
		loaderVersion = versions[0]
	}

	layout := s.layout(name)
	if layout.flat && (len(targets) > 1 || !samePlatformAsFlat(layout, targets[0])) {
		return fmt.Errorf("build %q uses the old single-platform layout; delete and re-prepare it to target several platforms", name)
	}

	var failures []string
	for _, target := range targets {
		key, _ := platform.Key(target)
		goos, arch, ok := platform.Mojang(target)
		if !ok {
			fmt.Fprintf(out, "Пропускаю %s: Minecraft не выпускает клиент под эту платформу.\n", key)
			continue
		}

		profileDir := filepath.Join(s.profilesDir, name, key)
		if layout.flat {
			profileDir = filepath.Join(s.profilesDir, name)
		}
		fmt.Fprintf(out, "Собираю «%s»: Minecraft %s, загрузчик %s %s, платформа %s…\n", name, id, orVanilla(loaderName), loaderVersion, key)
		if _, err := s.preparer.Prepare(ctx, prepare.Options{
			ProfileDir:    profileDir,
			VersionURL:    versionURL,
			OS:            goos,
			Arch:          arch,
			PlatformKey:   key,
			LoaderName:    loaderName,
			LoaderVersion: loaderVersion,
			JavaComponent: opts["java"],
		}); err != nil {
			fmt.Fprintf(out, "Платформа %s не собралась: %v\n", key, err)
			failures = append(failures, key)
			continue
		}
		if !layout.flat {
			if err := manifest.EnsureDefaultSettings(filepath.Join(s.profilesDir, name)); err != nil {
				return err
			}
			if err := manifest.SetLoader(filepath.Join(s.profilesDir, name), orVanilla(loaderName)); err != nil {
				return err
			}
		}
		fmt.Fprintf(out, "Готово: %s\n", profileDir)
	}
	prepared := s.layout(name)
	if !prepared.exists() {
		return fmt.Errorf("no platform could be prepared: %s", strings.Join(failures, ", "))
	}
	s.fire("build.prepared", name)
	if len(failures) > 0 {
		fmt.Fprintf(out, "Пропущено: %s\n", strings.Join(failures, ", "))
	}
	if prepared.flat {
		fmt.Fprintf(out, "Правьте сборку в %s, потом опубликуйте: publish %s\n", prepared.root, name)
	} else {
		fmt.Fprintf(out, "Настройки сборки — в %s, моды и файлы — в папке каждой платформы. Потом опубликуйте: publish %s\n", filepath.Join(s.profilesDir, name, manifest.SettingsFileName), name)
	}
	return nil
}

func samePlatformAsFlat(layout buildLayout, target corev1.Platform) bool {
	key, ok := platform.Key(target)
	if !ok {
		return false
	}
	profile, err := os.ReadFile(filepath.Join(layout.root, prepare.LaunchProfileName))
	if err != nil {
		return false
	}
	return strings.Contains(string(profile), "\"platformKey\": \""+key+"\"")
}

func parsePlatforms(raw string) ([]corev1.Platform, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		p, _ := platform.Parse(defaultPlatform)
		return []corev1.Platform{p}, nil
	}
	if strings.EqualFold(raw, "all") {
		return platform.Game(), nil
	}
	var out []corev1.Platform
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		p, ok := platform.Parse(part)
		if !ok {
			return nil, fmt.Errorf("unknown platform %q", part)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no platform given")
	}
	return out, nil
}

func (s *Service) publish(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: publish <name>")
	}
	name := args[0]
	if !safeName(name) {
		return fmt.Errorf("invalid build name %q", name)
	}
	layout := s.layout(name)
	if !layout.exists() {
		return fmt.Errorf("no prepared build named %q", name)
	}

	variants := layout.platforms
	if layout.flat {
		variants = []corev1.Platform{corev1.Platform_PLATFORM_UNSPECIFIED}
	}
	for _, variant := range variants {
		dir, settingsRoot := layout.dir(variant)
		published, err := prepare.PublishVariant(ctx, s.cas, s.signer, dir, settingsRoot, name, "1", variant)
		if err != nil {
			return err
		}
		target := filepath.Join(s.profilesDir, manifestName(name, variant))
		if err := os.WriteFile(target, published.Canonical, 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(target+".sig", published.Signature, 0o644); err != nil {
			return err
		}
		label, ok := platform.Key(variant)
		if !ok {
			label = "все платформы"
		}
		fmt.Fprintf(out, "Опубликована «%s» (%s): %s, %s\n", name, label, humanize.Count(len(published.Manifest.Files), "файл", "файла", "файлов"), humanize.Bytes(published.Manifest.TotalSize))
	}
	s.fire("build.published", name)
	return nil
}

func (s *Service) versionURL(ctx context.Context, mcVersion string) (string, string, error) {
	list, err := s.mojang.ListVersions(ctx)
	if err != nil {
		return "", "", err
	}
	switch mcVersion {
	case "release":
		mcVersion = list.Latest.Release
	case "snapshot":
		mcVersion = list.Latest.Snapshot
	}
	for _, version := range list.Versions {
		if version.ID == mcVersion {
			return version.URL, version.ID, nil
		}
	}
	return "", "", fmt.Errorf("unknown Minecraft version %q", mcVersion)
}

func parseKV(args []string) map[string]string {
	out := make(map[string]string, len(args))
	for _, arg := range args {
		if key, value, ok := strings.Cut(arg, "="); ok {
			out[key] = value
		}
	}
	return out
}

func orVanilla(name string) string {
	if name == "" {
		return "vanilla"
	}
	return name
}

func statusWord(status string) string {
	switch status {
	case "published":
		return "опубликована"
	case "prepared":
		return "собрана, ждёт публикации"
	default:
		return status
	}
}

func versionWord(kind string) string {
	switch kind {
	case "release":
		return "релиз"
	case "snapshot":
		return "снапшот"
	default:
		return kind
	}
}
