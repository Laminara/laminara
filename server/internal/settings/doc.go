package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/laminara/laminara/server/internal/config"
)

type Doc struct {
	path string
	mode os.FileMode
	tree map[string]any
}

func Open(path string) (*Doc, error) {
	if path == "" {
		return nil, errors.New("сервер запущен без файла настроек — правку негде хранить")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	tree := map[string]any{}
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, fmt.Errorf("%s не читается как JSON: %w", filepath.Base(path), err)
	}
	mode := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return &Doc{path: path, mode: mode, tree: tree}, nil
}

func (d *Doc) Path() string { return d.path }

func (d *Doc) raw(path string) (any, bool) {
	var current any = d.tree
	for _, step := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[step]
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func (d *Doc) put(path string, value any) error {
	steps := strings.Split(path, ".")
	var current any = d.tree
	for i, step := range steps[:len(steps)-1] {
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[step]
			if !ok || next == nil {
				if _, err := strconv.Atoi(steps[i+1]); err == nil {
					next = []any{}
				} else {
					next = map[string]any{}
				}
				node[step] = next
			}
			current = next
		case []any:
			index, err := strconv.Atoi(step)
			if err != nil || index < 0 || index >= len(node) {
				return fmt.Errorf("нет записи %s", path)
			}
			current = node[index]
		default:
			return fmt.Errorf("нет записи %s", path)
		}
	}
	last := steps[len(steps)-1]
	switch node := current.(type) {
	case map[string]any:
		if value == nil {
			delete(node, last)
			return nil
		}
		node[last] = value
		return nil
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(node) {
			return fmt.Errorf("нет записи %s", path)
		}
		node[index] = value
		return nil
	default:
		return fmt.Errorf("нет записи %s", path)
	}
}

func (d *Doc) drop(path string) error {
	steps := strings.Split(path, ".")
	parent := strings.Join(steps[:len(steps)-1], ".")
	last := steps[len(steps)-1]
	if parent == "" {
		delete(d.tree, last)
		return nil
	}
	holder, ok := d.raw(parent)
	if !ok {
		return nil
	}
	switch node := holder.(type) {
	case map[string]any:
		delete(node, last)
		return nil
	case []any:
		index, err := strconv.Atoi(last)
		if err != nil || index < 0 || index >= len(node) {
			return fmt.Errorf("нет записи %s", path)
		}
		return d.put(parent, append(append([]any{}, node[:index]...), node[index+1:]...))
	default:
		return fmt.Errorf("нет записи %s", path)
	}
}

func (d *Doc) Keys(path string) []string {
	holder, ok := d.raw(path)
	if !ok {
		return nil
	}
	switch node := holder.(type) {
	case map[string]any:
		keys := make([]string, 0, len(node))
		for key := range node {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return keys
	case []any:
		keys := make([]string, 0, len(node))
		for index := range node {
			keys = append(keys, strconv.Itoa(index))
		}
		return keys
	default:
		return nil
	}
}

func (d *Doc) Bytes() ([]byte, error) {
	data, err := json.MarshalIndent(d.tree, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (d *Doc) Save() error {
	data, err := d.Bytes()
	if err != nil {
		return err
	}
	var parsed config.Config
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("так настройки не читаются: %w", err)
	}
	temp := d.path + ".tmp"
	if err := os.WriteFile(temp, data, d.mode); err != nil {
		return err
	}
	return os.Rename(temp, d.path)
}
