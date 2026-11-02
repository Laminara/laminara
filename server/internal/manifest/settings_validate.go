package manifest

import (
	"fmt"
	"io/fs"
	"path/filepath"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/pathpolicy"
)

func ValidateSettings(sources []Sources, value Settings) error {
	if err := validateBuildArgs(value.JvmArgs, value.GameArgs, value.Classpath); err != nil {
		return err
	}
	if len(sources) == 0 {
		return validateFeatures(featureModelFromSpec(value.Features), map[string]corev1.FilePolicy{})
	}
	for _, source := range sources {
		policy := map[string]corev1.FilePolicy{}
		for index, root := range []string{source.Shared, source.Platform} {
			if index == 1 && root == source.Shared {
				continue
			}
			err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
				if walkErr != nil {
					return walkErr
				}
				if entry.IsDir() {
					if entry.Name() == internalDir || (index == 0 && path != root && entry.Name() == platformsDirName && filepath.Dir(path) == root) {
						return filepath.SkipDir
					}
					return nil
				}
				if entry.Name() == SettingsFileName {
					return nil
				}
				if entry.Type()&fs.ModeSymlink != 0 {
					return fmt.Errorf("в сборке есть ссылка %s; замените её настоящим файлом", path)
				}
				relative, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				relative = filepath.ToSlash(relative)
				policy[relative] = pathpolicy.Resolve(relative, value.UserWritable, value.Enforced)
				return nil
			})
			if err != nil {
				return err
			}
		}
		files := make([]*corev1.ManifestFile, 0, len(policy))
		for path, rule := range policy {
			files = append(files, &corev1.ManifestFile{Path: path, Policy: rule})
		}
		if err := validatePolicies(files); err != nil {
			return err
		}
		if err := validateFeatures(featureModelFromSpec(value.Features), policy); err != nil {
			return err
		}
	}
	return nil
}
