package buildsvc

import (
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/platform"
)

var platformOnly = map[string]bool{
	"runtime": true,
}

const migrationMark = ".laminara/migrating"

func migrationStarted(root string) bool {
	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(migrationMark)))
	return err == nil
}

func shareCommonFiles(root string, platforms []corev1.Platform) (uint64, error) {
	var freed uint64
	mark := filepath.Join(root, filepath.FromSlash(migrationMark))
	if err := os.MkdirAll(filepath.Dir(mark), 0o750); err != nil {
		return 0, err
	}
	if err := os.WriteFile(mark, []byte("перенос раскладки сборки начат"), 0o600); err != nil {
		return 0, err
	}
	for index, p := range platforms {
		key, ok := platform.Key(p)
		if !ok {
			continue
		}
		from := filepath.Join(root, key)
		to := filepath.Join(root, platformsDir, key)
		saved, err := sharePlatform(root, from, to, index == 0)
		if err != nil {
			return freed, err
		}
		freed += saved
		if err := os.RemoveAll(from); err != nil {
			return freed, err
		}
	}
	if err := os.Remove(mark); err != nil && !os.IsNotExist(err) {
		return freed, err
	}
	return freed, nil
}

func sharePlatform(root, from, to string, first bool) (uint64, error) {
	var freed uint64
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if belongsToPlatform(rel) {
			target = filepath.Join(to, rel)
		} else if !first {
			same, err := sameContent(path, target)
			if err != nil {
				return err
			}
			if same {
				if info, err := entry.Info(); err == nil {
					freed += uint64(info.Size())
				}
				return nil
			}
			target = filepath.Join(to, rel)
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
			return err
		}
		return os.Rename(path, target)
	})
	return freed, err
}

func belongsToPlatform(rel string) bool {
	head := rel
	for i := 0; i < len(rel); i++ {
		if os.IsPathSeparator(rel[i]) {
			head = rel[:i]
			break
		}
	}
	if platformOnly[head] {
		return true
	}
	return filepath.Base(rel) == manifest.LaunchProfileName
}

func sameContent(left, right string) (bool, error) {
	leftInfo, err := os.Stat(left)
	if err != nil {
		return false, err
	}
	rightInfo, err := os.Stat(right)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if leftInfo.Size() != rightInfo.Size() {
		return false, nil
	}
	leftSum, err := digestOf(left)
	if err != nil {
		return false, err
	}
	rightSum, err := digestOf(right)
	if err != nil {
		return false, err
	}
	return leftSum == rightSum, nil
}

func digestOf(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	sum := sha256.New()
	if _, err := io.Copy(sum, file); err != nil {
		return "", err
	}
	return string(sum.Sum(nil)), nil
}
