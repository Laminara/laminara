package settings

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/laminara/laminara/server/internal/config"
)

type Doc struct {
	path      string
	mode      os.FileMode
	tree      map[string]any
	revision  string
	namespace string
	schema    []Section
	validate  func([]byte) error

	once   sync.Once
	filled map[string]string
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
	return &Doc{path: path, mode: mode, tree: tree, revision: revisionOf(data)}, nil
}

var ErrChanged = errors.New("файл настроек изменился; загрузите новую версию перед сохранением")

func revisionOf(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func (d *Doc) Revision() string { return d.revision }

func (d *Doc) Path() string { return d.path }

func OpenSchema(path, namespace string, schema []Section, validate func([]byte) error) (*Doc, error) {
	doc, err := Open(path)
	if errors.Is(err, os.ErrNotExist) {
		doc = &Doc{path: path, mode: 0o600, tree: map[string]any{}, revision: revisionOf(nil)}
	} else if err != nil {
		return nil, err
	}
	doc.tree = map[string]any{namespace: doc.tree}
	doc.namespace, doc.schema, doc.validate = namespace, schema, validate
	return doc, nil
}

func (d *Doc) Sections() []Section {
	if d.schema != nil {
		return d.schema
	}
	return Sections()
}

func (d *Doc) sectionOf(key string) (Section, bool) {
	for _, section := range d.Sections() {
		if section.Key == key {
			return section, true
		}
	}
	return Section{}, false
}

func (d *Doc) raw(path string) (any, bool) {
	var current any = d.tree
	for _, step := range strings.Split(path, ".") {
		step = unescapeKey(step)
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
		step = unescapeKey(step)
		switch node := current.(type) {
		case map[string]any:
			next, ok := node[step]
			if !ok || next == nil {
				collection, known := d.findCollection(strings.Join(steps[:i+1], "."))
				if _, err := strconv.Atoi(steps[i+1]); err == nil && !(known && collection.Keyed) {
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
	last := unescapeKey(steps[len(steps)-1])
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
	last := unescapeKey(steps[len(steps)-1])
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
	var tree any = d.tree
	if d.namespace != "" {
		tree = d.tree[d.namespace]
	}
	data, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func (d *Doc) Validate() error {
	if err := d.validateExtensions(); err != nil {
		return err
	}
	data, err := d.Bytes()
	if err != nil {
		return err
	}
	if d.validate != nil {
		return d.validate(data)
	}
	parsed, err := config.Parse(data, d.path)
	if err != nil {
		return fmt.Errorf("так настройки не читаются: %w", err)
	}
	if err := config.Validate(parsed); err != nil {
		return fmt.Errorf("с такой настройкой сервер не поднимется — %w", err)
	}
	return nil
}

func (d *Doc) Save() error {
	if err := d.Validate(); err != nil {
		return err
	}
	data, err := d.Bytes()
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(d.path), ".laminara-settings-*")
	if err != nil {
		return fmt.Errorf("каталог настроек недоступен для записи: %w", err)
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err := file.Chmod(d.mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	current, err := os.ReadFile(d.path)
	if err != nil && !(errors.Is(err, os.ErrNotExist) && d.revision == revisionOf(nil)) {
		return err
	}
	if revisionOf(current) != d.revision {
		return ErrChanged
	}
	if err := os.Rename(temp, d.path); err != nil {
		return err
	}
	d.revision = revisionOf(data)
	return nil
}
