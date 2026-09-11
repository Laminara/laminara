package manifest

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/pathpolicy"
	"github.com/laminara/laminara/server/internal/progress"
	"github.com/laminara/laminara/server/internal/storage"
)

var launchCriticalPrefixes = []string{"mods/", "libraries/", "versions/", "runtime/", "assets/"}

func validatePolicies(files []*corev1.ManifestFile) error {
	var offenders []string
	for _, file := range files {
		if file.Policy != corev1.FilePolicy_FILE_POLICY_USER_WRITABLE {
			continue
		}
		critical := strings.HasSuffix(file.Path, ".jar") && !strings.Contains(file.Path, "/")
		for _, prefix := range launchCriticalPrefixes {
			if strings.HasPrefix(file.Path, prefix) {
				critical = true
				break
			}
		}
		if critical {
			offenders = append(offenders, file.Path)
		}
	}
	if len(offenders) > 0 {
		return fmt.Errorf("в userWritable попали файлы, без которых игра не запустится (моды, библиотеки, версии, runtime, ресурсы, jar в корне): %s", strings.Join(offenders, ", "))
	}
	return nil
}

const SchemaVersion = 1

const internalDir = ".laminara"

type Builder struct {
	cas *storage.CAS
	now func() time.Time
}

func NewBuilder(cas *storage.CAS) *Builder {
	return &Builder{cas: cas, now: time.Now}
}

func (b *Builder) Build(ctx context.Context, root, modpack, version string) (*corev1.Manifest, error) {
	return b.BuildVariant(ctx, root, root, modpack, version, corev1.Platform_PLATFORM_UNSPECIFIED)
}

func (b *Builder) BuildVariant(ctx context.Context, root, settingsRoot, modpack, version string, platform corev1.Platform) (*corev1.Manifest, error) {
	return b.BuildPlatform(ctx, Sources{Shared: root, Platform: root}, settingsRoot, modpack, version, platform)
}

type Sources struct {
	Shared   string
	Platform string
}

func (b *Builder) BuildPlatform(ctx context.Context, sources Sources, settingsRoot, modpack, version string, platform corev1.Platform) (*corev1.Manifest, error) {
	settings, err := LoadSettings(settingsRoot)
	if err != nil {
		return nil, err
	}

	needed, err := neededLibraries(sources.Platform, settings)
	if err != nil {
		return nil, err
	}
	collected := map[string]*corev1.ManifestFile{}
	var order []string
	var indexed int

	take := func(root string, skipShared bool) error {
		return filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == internalDir {
					return filepath.SkipDir
				}
				if skipShared && p != root && d.Name() == platformsDirName && filepath.Dir(p) == root {
					return filepath.SkipDir
				}
				return nil
			}
			if d.Name() == SettingsFileName {
				return nil
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("в сборке есть ссылка %s — публикуются только настоящие файлы, замените её копией", p)
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			slashPath := filepath.ToSlash(rel)
			if skipShared && needed != nil && strings.HasPrefix(slashPath, librariesDir+"/") && !needed[slashPath] {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				return err
			}
			ref, err := b.cas.PutFile(ctx, p)
			if err != nil {
				return err
			}
			if _, seen := collected[slashPath]; !seen {
				order = append(order, slashPath)
			}
			collected[slashPath] = &corev1.ManifestFile{
				Path:       slashPath,
				Object:     ref,
				Executable: info.Mode()&0o111 != 0,
				Policy:     pathpolicy.Resolve(slashPath, settings.UserWritable, settings.Enforced),
			}
			indexed++
			progress.Report(ctx, progress.Event{
				Phase:   "Индексация файлов",
				Message: humanize.Count(indexed, "файл", "файла", "файлов"),
			})
			return nil
		})
	}

	if err := take(sources.Shared, true); err != nil {
		return nil, err
	}
	if sources.Platform != sources.Shared {
		if err := take(sources.Platform, false); err != nil {
			return nil, err
		}
	}

	files := make([]*corev1.ManifestFile, 0, len(order))
	var total uint64
	for _, path := range order {
		file := collected[path]
		files = append(files, file)
		total += file.Object.Size
	}
	if err := validatePolicies(files); err != nil {
		return nil, err
	}

	model := featureModelFromSpec(settings.Features)
	if model != nil {
		policyByPath := make(map[string]corev1.FilePolicy, len(files))
		sizeByPath := make(map[string]uint64, len(files))
		for _, file := range files {
			policyByPath[file.Path] = file.Policy
			sizeByPath[file.Path] = file.Object.Size
		}
		if err := validateFeatures(model, policyByPath); err != nil {
			return nil, err
		}
		computeAddedSizes(model.Groups, sizeByPath)
	}

	if err := validateBuildArgs(settings.JvmArgs, settings.GameArgs, settings.Classpath); err != nil {
		return nil, err
	}

	launch, err := readLaunchProfile(sources.Platform)
	if err != nil {
		return nil, err
	}

	return &corev1.Manifest{
		SchemaVersion:        SchemaVersion,
		Modpack:              modpack,
		Version:              version,
		MinecraftVersion:     minecraftVersionOf(launch),
		JavaMajor:            uint32(launch.JavaMajor),
		GeneratedAtUnixNanos: b.now().UnixNano(),
		Files:                files,
		TotalSize:            total,
		UserWritable:         settings.UserWritable,
		Enforced:             settings.Enforced,
		ServerAddress:        settings.ServerAddress,
		Loader:               settings.Loader,
		Features:             model,
		Platform:             platform,
		JvmArgs:              settings.JvmArgs,
		GameArgs:             settings.GameArgs,
		Classpath:            settings.Classpath,
		ClasspathExclude:     settings.ClasspathExclude,
		MainClass:            settings.MainClass,
	}, nil
}

const (
	platformsDirName = "platforms"
	librariesDir     = "libraries"
)

func neededLibraries(platformDir string, settings Settings) (map[string]bool, error) {
	launch, err := readLaunchProfile(platformDir)
	if err != nil {
		return nil, err
	}
	needed := make(map[string]bool, len(launch.Classpath)+len(launch.Natives))
	for _, group := range [][]string{launch.Classpath, launch.Natives, settings.Classpath} {
		for _, path := range group {
			needed[path] = true
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	return needed, nil
}
