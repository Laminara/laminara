package catalog

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/atomicfile"
	"github.com/laminara/laminara/server/internal/platform"
	"google.golang.org/protobuf/proto"
)

type ReleaseFile struct {
	Name      string
	Canonical []byte
	Signature []byte
}

type ReleasePointer struct {
	Version          int      `json:"version"`
	Current          string   `json:"current"`
	Retained         []string `json:"retained,omitempty"`
	SettingsRevision string   `json:"settingsRevision,omitempty"`
	Workspace        string   `json:"workspace,omitempty"`
	InputDigest      string   `json:"inputDigest,omitempty"`
	SourceRevision   string   `json:"sourceRevision,omitempty"`
	WorkspaceDigest  string   `json:"workspaceDigest,omitempty"`
}

func releaseRoot(dir, build string) string {
	return filepath.Join(dir, build, ".laminara")
}

func safeComponent(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/\\:\x00")
}

func ReadRelease(dir, build string) (*ReleasePointer, error) {
	if !safeComponent(build) {
		return nil, fmt.Errorf("недопустимое имя сборки")
	}
	data, err := os.ReadFile(filepath.Join(releaseRoot(dir, build), "current.json"))
	if err != nil {
		return nil, err
	}
	var pointer ReleasePointer
	if err := json.Unmarshal(data, &pointer); err != nil {
		return nil, err
	}
	if pointer.Version != 1 || !safeComponent(pointer.Current) || (pointer.Workspace != "" && !safeComponent(pointer.Workspace)) {
		return nil, fmt.Errorf("неверный указатель публикации %s", build)
	}
	seen := map[string]bool{pointer.Current: true}
	for _, id := range pointer.Retained {
		if !safeComponent(id) || seen[id] {
			return nil, fmt.Errorf("неверная история публикаций %s", build)
		}
		seen[id] = true
	}
	return &pointer, nil
}

func PublishRelease(dir, build string, files []ReleaseFile, revision string) error {
	return publishRelease(dir, build, files, revision, func(path string, data []byte) error { return atomicfile.Write(path, data, 0o644) })
}

func PublishReleaseWithMetadata(dir, build string, files []ReleaseFile, metadata ReleasePointer) error {
	return publishReleaseWithMetadata(dir, build, files, metadata, func(path string, data []byte) error { return atomicfile.Write(path, data, 0o644) })
}

func publishRelease(dir, build string, files []ReleaseFile, revision string, commit func(string, []byte) error) error {
	metadata := ReleasePointer{SettingsRevision: revision}
	if previous, err := ReadRelease(dir, build); err == nil {
		metadata.Workspace = previous.Workspace
	}
	return publishReleaseWithMetadata(dir, build, files, metadata, commit)
}

func publishReleaseWithMetadata(dir, build string, files []ReleaseFile, metadata ReleasePointer, commit func(string, []byte) error) error {
	if !safeComponent(build) || len(files) == 0 {
		return fmt.Errorf("публикация должна содержать сборку и платформы")
	}
	seen := map[string]bool{}
	for _, file := range files {
		var m corev1.Manifest
		if err := proto.Unmarshal(file.Canonical, &m); err != nil {
			return err
		}
		if m.Modpack != build || file.Name != ManifestName(build, m.Platform) || len(file.Signature) == 0 || seen[file.Name] {
			return fmt.Errorf("недопустимый файл публикации %s", file.Name)
		}
		seen[file.Name] = true
	}
	root := releaseRoot(dir, build)
	for _, path := range []string{filepath.Join(dir, build), root, filepath.Join(root, "releases")} {
		if err := os.Mkdir(path, 0o755); err != nil && !os.IsExist(err) {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("папка публикации небезопасна: %s", path)
		}
	}
	previous, err := ReadRelease(dir, build)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var retained []string
	if previous != nil {
		retained = append([]string{previous.Current}, previous.Retained...)
	} else {
		legacy, err := legacyRelease(dir, build)
		if err != nil {
			return err
		}
		if len(legacy) > 0 {
			id, err := writeRelease(root, legacy)
			if err != nil {
				return err
			}
			if _, err := writeReleaseMetadata(root, ReleasePointer{Version: 1, Current: id}); err != nil {
				return err
			}
			retained = append(retained, id)
		}
	}
	id, err := writeRelease(root, files)
	if err != nil {
		return err
	}
	pointer := metadata
	pointer.Version, pointer.Current, pointer.Retained = 1, id, retained
	data, err := writeReleaseMetadata(root, pointer)
	if err != nil {
		return err
	}
	if err := commit(filepath.Join(root, "current.json"), data); err != nil {
		return err
	}
	return syncDirectory(root)
}

func writeReleaseMetadata(root string, pointer ReleasePointer) ([]byte, error) {
	data, err := json.Marshal(pointer)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "releases", pointer.Current)
	if err := atomicfile.Write(filepath.Join(dir, "release.json"), data, 0o644); err != nil {
		return nil, err
	}
	if err := syncDirectory(dir); err != nil {
		return nil, err
	}
	return data, nil
}

func writeRelease(root string, files []ReleaseFile) (string, error) {
	id := rand.Text()
	dir := filepath.Join(root, "releases", id)
	if err := os.Mkdir(dir, 0o755); err != nil {
		return "", err
	}
	inventory := map[string]string{}
	for _, file := range files {
		for _, entry := range []struct {
			name string
			data []byte
		}{{file.Name, file.Canonical}, {SignaturePath(file.Name), file.Signature}} {
			if err := atomicfile.Write(filepath.Join(dir, entry.name), entry.data, 0o644); err != nil {
				return "", err
			}
			inventory[entry.name] = fmt.Sprintf("%x", sha256.Sum256(entry.data))
		}
	}
	data, err := json.Marshal(inventory)
	if err != nil {
		return "", err
	}
	if err := atomicfile.Write(filepath.Join(dir, "inventory.json"), data, 0o644); err != nil {
		return "", err
	}
	if err := syncDirectory(dir); err != nil {
		return "", err
	}
	if err := syncDirectory(filepath.Dir(dir)); err != nil {
		return "", err
	}
	return id, nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func legacyRelease(dir, build string) ([]ReleaseFile, error) {
	var files []ReleaseFile
	for _, p := range append([]corev1.Platform{corev1.Platform_PLATFORM_UNSPECIFIED}, platform.Game()...) {
		name := ManifestName(build, p)
		data, err := os.ReadFile(filepath.Join(dir, name))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		sig, err := os.ReadFile(filepath.Join(dir, SignaturePath(name)))
		if err != nil {
			return nil, err
		}
		files = append(files, ReleaseFile{Name: name, Canonical: data, Signature: sig})
	}
	return files, nil
}
