package catalog

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/laminara/laminara/server/internal/atomicfile"
)

func VerifyRelease(dir string) error {
	data, err := os.ReadFile(filepath.Join(dir, "inventory.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var inventory map[string]string
	if err := json.Unmarshal(data, &inventory); err != nil {
		return err
	}
	if len(inventory) == 0 {
		return errors.New("пуста опись публикации")
	}
	for name, expected := range inventory {
		if !safeComponent(name) {
			return errors.New("неверное имя в описи публикации")
		}
		contents, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(contents)) != expected {
			return errors.New("публикация повреждена")
		}
	}
	files, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, file := range files {
		if strings.HasSuffix(file.Name(), manifestExt) && inventory[file.Name()] == "" {
			return errors.New("лишний манифест в публикации")
		}
	}
	return nil
}

func RollbackRelease(dir, build, id string) error {
	current, err := ReadRelease(dir, build)
	if err != nil {
		return err
	}
	if !slices.Contains(current.Retained, id) {
		return errors.New("публикации нет в истории этой сборки")
	}
	root := filepath.Join(releaseRoot(dir, build), "releases", id)
	if err := VerifyRelease(root); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(root, "release.json"))
	if err != nil {
		return err
	}
	var previous ReleasePointer
	if err := json.Unmarshal(data, &previous); err != nil {
		return err
	}
	if previous.Current != id || previous.Version != 1 || (previous.Workspace != "" && !safeComponent(previous.Workspace)) {
		return errors.New("повреждены сведения публикации")
	}
	previous.Retained = []string{current.Current}
	for _, retained := range current.Retained {
		if retained != id {
			previous.Retained = append(previous.Retained, retained)
		}
	}
	data, err = json.Marshal(previous)
	if err != nil {
		return err
	}
	return atomicfile.Write(filepath.Join(releaseRoot(dir, build), "current.json"), data, 0o644)
}
