package safepath

import (
	"fmt"
	"path/filepath"
	"strings"
)

func Join(root, relative string) (string, error) {
	cleaned, err := Relative(relative)
	if err != nil {
		return "", err
	}
	target := filepath.Join(root, cleaned)
	base := filepath.Clean(root)
	if target != base && !strings.HasPrefix(target, base+string(filepath.Separator)) {
		return "", fmt.Errorf("путь %q уводит за пределы %s", relative, root)
	}
	return target, nil
}

func Relative(path string) (string, error) {
	text := strings.ReplaceAll(strings.TrimSpace(path), "\\", "/")
	if text == "" {
		return "", fmt.Errorf("пустой путь")
	}
	if strings.HasPrefix(text, "/") {
		return "", fmt.Errorf("путь %q абсолютный", path)
	}
	if len(text) > 1 && text[1] == ':' {
		return "", fmt.Errorf("путь %q указывает на диск", path)
	}
	if strings.ContainsRune(text, 0) {
		return "", fmt.Errorf("путь %q содержит нулевой байт", path)
	}

	var parts []string
	for _, segment := range strings.Split(text, "/") {
		switch segment {
		case "", ".":
			continue
		case "..":
			return "", fmt.Errorf("путь %q выходит вверх по дереву", path)
		default:
			parts = append(parts, segment)
		}
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("путь %q ни на что не указывает", path)
	}
	return filepath.Join(parts...), nil
}
