package buildsvc

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/admin"
	"github.com/laminara/laminara/server/internal/atomicfile"
	"github.com/laminara/laminara/server/internal/buildview"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/command"
	"github.com/laminara/laminara/server/internal/compat"
	"github.com/laminara/laminara/server/internal/httpx"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/loader"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/mojang"
	"github.com/laminara/laminara/server/internal/platform"
	"github.com/laminara/laminara/server/internal/prepare"
	"github.com/laminara/laminara/server/internal/storage"
)

type Service struct {
	busy        sync.Map
	mojang      *mojang.Client
	preparer    *prepare.Preparer
	cas         *storage.CAS
	signer      *manifest.Signer
	profilesDir string
	published   *catalog.Catalog
	access      func(build string) string
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

func (s *Service) SetCatalog(published *catalog.Catalog) {
	s.published = published
}

func (s *Service) SetAccess(describe func(build string) string) {
	s.access = describe
}

func (s *Service) SetEmitter(emit func(topic string, data map[string]string)) {
	s.emit = emit
}

func (s *Service) alone(name string) (func(), error) {
	if _, taken := s.busy.LoadOrStore(name, struct{}{}); taken {
		return nil, fmt.Errorf("со сборкой «%s» прямо сейчас работает другая команда — дождитесь, пока она закончит", name)
	}
	return func() { s.busy.Delete(name) }, nil
}

func (s *Service) fire(topic, name string) {
	if s.emit != nil {
		s.emit(topic, map[string]string{"name": name})
	}
}

const defaultPlatform = "windows-x64"

func (s *Service) Commands() []command.Command {
	return []command.Command{
		{Name: "versions", Synopsis: "версии Minecraft (versions [фильтр])", Run: s.versions},
		{Name: "loaders", Synopsis: "загрузчики модов для версии (loaders <версия>)", Run: s.loaders},
		{Name: "install", Aliases: []string{"prepare"}, Synopsis: "собрать клиент (install <имя> <версия> [loader=..] [loaderVersion=..] [compat=..] [platform=..] [java=..])", Run: s.prepare},
		{Name: "publish", Aliases: []string{"release"}, Synopsis: "опубликовать сборку — лаунчеры увидят её (publish <имя>)", Run: s.publish},
		{Name: "builds", Aliases: []string{"clients"}, Synopsis: "сборки проекта и их состояние", Run: s.builds},
		{Name: "build", Aliases: []string{"info"}, Synopsis: "всё об одной сборке (build <имя>)", Run: s.buildInfo},
		{Name: "players", Aliases: []string{"online", "who"}, Synopsis: "кто сейчас в игре на серверах сборок", Run: s.players},
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
		fmt.Fprintf(out, "%-20s %-26s %s\n", build.Name, buildview.StatusWord(build.Status), buildLine(build))
	}
	return nil
}

func buildLine(build admin.BuildEntry) string {
	if build.Trouble != "" {
		return build.Trouble
	}
	if build.Minecraft == "" {
		return ""
	}
	return fmt.Sprintf("%-8s %-9s %s", build.Minecraft, buildview.LoaderWord(build.Loader), humanize.Bytes(build.SizeBytes))
}

func (s *Service) manifestsOf(name string) []string {
	var found []string
	flat := catalog.ManifestPath(s.profilesDir, name, corev1.Platform_PLATFORM_UNSPECIFIED)
	if _, err := os.Stat(flat); err == nil {
		found = append(found, flat)
	}
	for _, p := range platform.Game() {
		if _, ok := platform.Key(p); !ok {
			continue
		}
		candidate := catalog.ManifestPath(s.profilesDir, name, p)
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
	details := s.details()
	var builds []admin.BuildEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		build := admin.BuildEntry{Name: name, Status: "prepared", Prepared: s.layout(name).platforms}
		if recipe, err := s.rememberedRecipe(name); err == nil {
			build.Compat = recipe
		} else {
			build.Trouble = err.Error()
		}
		if len(s.manifestsOf(name)) > 0 {
			build.Status = "published"
		}
		fillPublished(&build, details[name])
		if s.access != nil {
			build.Access = s.access(name)
		}
		builds = append(builds, build)
	}
	return builds, nil
}

func (s *Service) details() map[string][]catalog.Variant {
	if s.published == nil {
		return nil
	}
	list, err := s.published.Details()
	if err != nil {
		return nil
	}
	out := make(map[string][]catalog.Variant, len(list))
	for _, detail := range list {
		out[detail.Name] = detail.Variants
	}
	return out
}

func fillPublished(build *admin.BuildEntry, variants []catalog.Variant) {
	if len(variants) == 0 {
		return
	}
	build.Status = "published"
	head := variants[0]
	build.Minecraft = head.Minecraft
	build.JavaMajor = head.JavaMajor
	build.Loader = head.Loader
	build.SizeBytes = head.TotalSize
	build.Files = head.Files
	build.ServerAddress = head.ServerAddress
	build.HasFeatures = head.HasFeatures
	for _, variant := range variants {
		if variant.Platform != corev1.Platform_PLATFORM_UNSPECIFIED {
			build.Published = append(build.Published, variant.Platform)
		}
		if variant.PublishedAt.After(build.PublishedAt) {
			build.PublishedAt = variant.PublishedAt
		}
	}
}

func (s *Service) delete(_ context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("напишите имя сборки: delete <имя>")
	}
	name := args[0]
	if !safeName(name) {
		return fmt.Errorf("имя «%s» не годится для сборки — без слэшей и точек", name)
	}
	done, err := s.alone(name)
	if err != nil {
		return err
	}
	defer done()

	dir := filepath.Join(s.profilesDir, name)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("сборки «%s» нет — список даёт команда builds", name)
	}
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	var left []string
	for _, manifestPath := range s.manifestsOf(name) {
		for _, path := range []string{manifestPath, catalog.SignaturePath(manifestPath)} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				left = append(left, path)
			}
		}
	}
	s.fire("build.deleted", name)
	if len(left) > 0 {
		return fmt.Errorf("файлы сборки удалены, но манифесты остались и игроки всё ещё видят «%s» — уберите руками: %s", name, strings.Join(left, ", "))
	}
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
		return fmt.Errorf("напишите версию Minecraft: loaders <версия>")
	}
	loaders, err := s.Loaders(ctx, args[0])
	if err != nil {
		return err
	}
	for _, entry := range loaders {
		switch {
		case entry.Trouble != "":
			fmt.Fprintf(out, "%-10s список версий не пришёл: %s\n", entry.Name, entry.Trouble)
		case len(entry.Versions) == 0:
			fmt.Fprintln(out, entry.Name)
		default:
			fmt.Fprintf(out, "%-10s последняя %s, всего %s\n", entry.Name, entry.Versions[0], humanize.Count(len(entry.Versions), "версия", "версии", "версий"))
		}
	}
	writeRecipes(out, compat.For(args[0]))
	return nil
}

func (s *Service) Loaders(ctx context.Context, mcVersion string) ([]admin.LoaderEntry, error) {
	loaders := []admin.LoaderEntry{{Name: "vanilla"}}
	for _, l := range loader.All() {
		versions, err := l.Versions(ctx, mcVersion)
		if err != nil {
			if !httpx.NothingThere(err) {
				loaders = append(loaders, admin.LoaderEntry{Name: l.Name(), Trouble: err.Error()})
			}
			continue
		}
		if len(versions) == 0 {
			continue
		}
		loaders = append(loaders, admin.LoaderEntry{Name: l.Name(), Versions: versions})
	}
	return loaders, nil
}

func (s *Service) Recipes(mcVersion string) []admin.RecipeEntry {
	recipes := compat.For(mcVersion)
	entries := make([]admin.RecipeEntry, 0, len(recipes))
	for _, recipe := range recipes {
		entries = append(entries, admin.RecipeEntry{Name: recipe.Name(), Summary: recipe.Summary(), Loader: recipe.Loader()})
	}
	return entries
}

func (s *Service) prepare(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 2 {
		return fmt.Errorf("напишите имя и версию: install <имя> <версия> [loader=..] [loaderVersion=..] [compat=..] [platform=..] [java=..]")
	}
	name, mcVersion := args[0], args[1]
	if !safeName(name) {
		return fmt.Errorf("имя «%s» не годится для сборки — без слэшей и точек", name)
	}
	done, err := s.alone(name)
	if err != nil {
		return err
	}
	defer done()
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

	plan, err := s.compatPlan(ctx, name, opts, id, out)
	if err != nil {
		return err
	}
	if plan != nil {
		announceRecipe(out, plan, loaderName, loaderVersion)
		if java := opts["java"]; java != "" {
			fmt.Fprintf(out, "Java взята ваша (%s), а не та, что просит рецепт — на ней он может не запуститься.\n", java)
		}
		versionURL = plan.VersionURL
		loaderName = plan.LoaderName
		loaderVersion = plan.LoaderVersion
	} else if loaderName != "" && loaderName != "vanilla" && loaderVersion == "" {
		l, ok := loader.Get(loaderName)
		if !ok {
			return fmt.Errorf("загрузчика «%s» нет — какие есть для версии, покажет loaders <версия>", loaderName)
		}
		versions, err := l.Versions(ctx, id)
		if err != nil {
			return err
		}
		if len(versions) == 0 {
			return fmt.Errorf("у загрузчика «%s» нет версий под Minecraft %s", loaderName, id)
		}
		loaderVersion = versions[0]
	}
	if plan == nil && loader.Prerelease(loaderVersion) {
		fmt.Fprintf(out, "Загрузчик %s %s — предварительная версия, сбои в ней ожидаемы.\n", buildview.LoaderWord(loaderName), loaderVersion)
	}

	layout := s.layout(name)
	if layout.flat && (len(targets) > 1 || !samePlatformAsFlat(layout, targets[0])) {
		return fmt.Errorf("сборка «%s» сделана по старой схеме с одной платформой — удалите её и соберите заново, чтобы добавить платформы", name)
	}
	if !layout.flat && !layout.shared && len(layout.platforms) > 0 {
		freed, err := shareCommonFiles(filepath.Join(s.profilesDir, name), layout.platforms)
		if err != nil {
			return fmt.Errorf("не вышло свести общие файлы платформ в одну папку: %w", err)
		}
		fmt.Fprintf(out, "Общие файлы платформ сведены в одну папку, освобождено %s.\n", humanize.Bytes(freed))
		layout = s.layout(name)
	}

	var failures []string
	built := 0
	for _, target := range targets {
		key, _ := platform.Key(target)
		goos, arch, ok := platform.Mojang(target)
		if !ok {
			fmt.Fprintf(out, "Пропускаю %s: Minecraft не выпускает клиент под эту платформу.\n", key)
			continue
		}

		placed := sharedPlacement(filepath.Join(s.profilesDir, name), target)
		if layout.flat {
			root := filepath.Join(s.profilesDir, name)
			placed = placement{shared: root, platform: root}
		}
		fmt.Fprintf(out, "Собираю «%s»: Minecraft %s, загрузчик %s %s, платформа %s…\n", name, id, buildview.LoaderWord(loaderName), loaderVersion, key)
		if _, err := s.preparer.Prepare(ctx, prepare.Options{
			ProfileDir:       placed.shared,
			PlatformDir:      placed.platform,
			VersionURL:       versionURL,
			OS:               goos,
			Arch:             arch,
			PlatformKey:      key,
			MinecraftVersion: id,
			LoaderName:       loaderName,
			LoaderVersion:    loaderVersion,
			LoaderInManifest: plan != nil,
			JavaComponent:    opts["java"],
			Files:            planFiles(plan),
		}); err != nil {
			fmt.Fprintf(out, "Платформа %s не собралась: %v\n", key, err)
			failures = append(failures, key)
			continue
		}
		if !layout.flat {
			if err := manifest.EnsureDefaultSettings(filepath.Join(s.profilesDir, name)); err != nil {
				return err
			}
			if err := manifest.SetLoader(filepath.Join(s.profilesDir, name), buildview.LoaderWord(loaderName)); err != nil {
				return err
			}
		}
		built++
		fmt.Fprintf(out, "Готово: %s\n", placed.platform)
	}
	prepared := s.layout(name)
	if built == 0 || !prepared.exists() {
		return fmt.Errorf("ни одну платформу собрать не вышло: %s", strings.Join(failures, ", "))
	}
	if err := s.rememberRecipe(prepared.root, plan, out); err != nil {
		return err
	}
	s.fire("build.prepared", name)
	if len(failures) > 0 {
		fmt.Fprintf(out, "Пропущено: %s\n", strings.Join(failures, ", "))
	}
	if prepared.flat {
		fmt.Fprintf(out, "Правьте сборку в %s, потом опубликуйте: publish %s\n", prepared.root, name)
	} else {
		fmt.Fprintf(out, "Моды и файлы кладите в %s — они пойдут на все платформы.\n", prepared.root)
		fmt.Fprintf(out, "Что нужно только одной платформе — в %s.\n", filepath.Join(prepared.root, platformsDir, "<платформа>"))
		fmt.Fprintf(out, "Настройки сборки — в %s. Потом опубликуйте: publish %s\n", filepath.Join(prepared.root, manifest.SettingsFileName), name)
	}
	return nil
}

func samePlatformAsFlat(layout buildLayout, target corev1.Platform) bool {
	key, ok := platform.Key(target)
	if !ok {
		return false
	}
	profile, err := os.ReadFile(filepath.Join(layout.root, manifest.LaunchProfileName))
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
			return nil, fmt.Errorf("платформы «%s» не существует — возьмите windows-x64, linux, mac-os или all", part)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("не указана ни одна платформа")
	}
	return out, nil
}

type readyManifest struct {
	target    string
	canonical []byte
	signature []byte
	label     string
	files     int
	size      uint64
}

func (s *Service) publish(ctx context.Context, args []string, out io.Writer) error {
	if len(args) < 1 {
		return fmt.Errorf("напишите имя сборки: publish <имя>")
	}
	name := args[0]
	if !safeName(name) {
		return fmt.Errorf("имя «%s» не годится для сборки — без слэшей и точек", name)
	}
	done, err := s.alone(name)
	if err != nil {
		return err
	}
	defer done()

	layout := s.layout(name)
	if !layout.exists() {
		return fmt.Errorf("собранной сборки «%s» нет — сначала install", name)
	}

	variants := layout.platforms
	if layout.flat {
		variants = []corev1.Platform{corev1.Platform_PLATFORM_UNSPECIFIED}
	}
	prepared := make([]readyManifest, 0, len(variants))
	for _, variant := range variants {
		placed := layout.place(variant)
		published, err := prepare.PublishPlatform(ctx, s.cas, s.signer,
			manifest.Sources{Shared: placed.shared, Platform: placed.platform},
			layout.root, name, "1", variant)
		if err != nil {
			return err
		}
		label, ok := platform.Key(variant)
		if !ok {
			label = "все платформы"
		}
		prepared = append(prepared, readyManifest{
			target:    catalog.ManifestPath(s.profilesDir, name, variant),
			canonical: published.Canonical,
			signature: published.Signature,
			label:     label,
			files:     len(published.Manifest.Files),
			size:      published.Manifest.TotalSize,
		})
	}
	for _, item := range prepared {
		if err := atomicfile.Write(item.target, item.canonical, 0o644); err != nil {
			return err
		}
		if err := atomicfile.Write(catalog.SignaturePath(item.target), item.signature, 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "Опубликована «%s» (%s): %s, %s\n", name, item.label, humanize.Count(item.files, "файл", "файла", "файлов"), humanize.Bytes(item.size))
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
	return "", "", fmt.Errorf("версии Minecraft «%s» нет — список даёт команда versions", mcVersion)
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
