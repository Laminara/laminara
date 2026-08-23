package manifest

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
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
		return fmt.Errorf("user_writable must not cover launch-critical files (mods/libraries/versions/runtime/assets/root jars): %s", strings.Join(offenders, ", "))
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
	settings, err := LoadSettings(settingsRoot)
	if err != nil {
		return nil, err
	}

	fileCount := int64(0)
	_ = filepath.WalkDir(root, func(_ string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == internalDir {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == SettingsFileName {
			return nil
		}
		fileCount++
		return nil
	})
	progress.Report(ctx, progress.Event{Phase: "Индексация файлов", Total: fileCount})

	var files []*corev1.ManifestFile
	var total uint64
	var indexed int64
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if d.Name() == internalDir {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == SettingsFileName {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		file, err := os.Open(p)
		if err != nil {
			return err
		}
		ref, err := b.cas.Put(ctx, file)
		file.Close()
		if err != nil {
			return err
		}
		slashPath := filepath.ToSlash(rel)
		files = append(files, &corev1.ManifestFile{
			Path:       slashPath,
			Object:     ref,
			Executable: info.Mode()&0o111 != 0,
			Policy:     pathpolicy.Resolve(slashPath, settings.UserWritable, settings.Enforced),
		})
		total += ref.Size
		indexed++
		progress.Report(ctx, progress.Event{Phase: "Индексация файлов", Current: indexed, Total: fileCount})
		return nil
	})
	if err != nil {
		return nil, err
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

	return &corev1.Manifest{
		SchemaVersion:        SchemaVersion,
		Modpack:              modpack,
		Version:              version,
		GeneratedAtUnixNanos: b.now().UnixNano(),
		Files:                files,
		TotalSize:            total,
		UserWritable:         settings.UserWritable,
		Enforced:             settings.Enforced,
		ServerAddress:        settings.ServerAddress,
		Loader:               settings.Loader,
		Features:             model,
		Platform:             platform,
	}, nil
}
