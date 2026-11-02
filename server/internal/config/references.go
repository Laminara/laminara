package config

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func Resolve(data []byte, path string) ([]byte, error) {
	var tree any
	if err := json.Unmarshal(data, &tree); err != nil {
		return nil, err
	}
	resolved, err := resolveValue(tree, filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	return json.Marshal(resolved)
}

func resolveValue(value any, base string) (any, error) {
	switch node := value.(type) {
	case map[string]any:
		for key, child := range node {
			resolved, err := resolveValue(child, base)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", key, err)
			}
			node[key] = resolved
		}
	case []any:
		for index, child := range node {
			resolved, err := resolveValue(child, base)
			if err != nil {
				return nil, fmt.Errorf("элемент %d: %w", index, err)
			}
			node[index] = resolved
		}
	case string:
		if !strings.HasPrefix(node, "${") || !strings.HasSuffix(node, "}") {
			return node, nil
		}
		kind, reference, ok := strings.Cut(node[2:len(node)-1], ":")
		if !ok || (kind != "env" && kind != "file") {
			return node, nil
		}
		if reference == "" {
			return nil, fmt.Errorf("пустая ссылка %s", kind)
		}
		if kind == "env" {
			resolved, exists := os.LookupEnv(reference)
			if !exists {
				return nil, fmt.Errorf("не задана переменная окружения %s", reference)
			}
			return resolved, nil
		}
		path := reference
		if !filepath.IsAbs(path) {
			path = filepath.Join(base, path)
		}
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("не удалось открыть файл секрета %s", reference)
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, (1<<20)+1))
		if err != nil || len(data) > 1<<20 {
			return nil, fmt.Errorf("файл секрета %s не читается или больше 1 МиБ", reference)
		}
		return strings.TrimRight(string(data), "\r\n"), nil
	}
	return value, nil
}
