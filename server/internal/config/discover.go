package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const FileName = "config.json"

func SearchPaths() []string {
	paths := []string{filepath.Join("/etc", "laminara", FileName)}
	if home, err := os.UserConfigDir(); err == nil {
		paths = append(paths, filepath.Join(home, "laminara", FileName))
	}
	if working, err := os.Getwd(); err == nil {
		paths = append(paths, filepath.Join(working, FileName))
	}
	return paths
}

func Discover(chosen string) (string, error) {
	if chosen != "" {
		return chosen, nil
	}
	searched := SearchPaths()
	for _, path := range searched {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", fmt.Errorf("не нашёл конфиг сервера — укажите его флагом --config <путь>; искал тут: %s", strings.Join(searched, ", "))
}
