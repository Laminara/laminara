package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/config"
)

func TestAnExplicitPathWins(t *testing.T) {
	got, err := config.Discover("/своё/место/config.json")
	if err != nil || got != "/своё/место/config.json" {
		t.Fatalf("указанный путь должен возвращаться как есть: %q %v", got, err)
	}
}

func TestTheConfigNextToTheCommandIsFound(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, config.FileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	back, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(back) })

	got, err := config.Discover("")
	if err != nil {
		t.Fatalf("конфиг рядом не нашёлся: %v", err)
	}
	if filepath.Base(got) != config.FileName {
		t.Fatalf("нашлось не то: %q", got)
	}
}

func TestWhenNothingIsFoundTheErrorSaysWhereItLooked(t *testing.T) {
	dir := t.TempDir()
	back, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(back) })

	_, err = config.Discover("")
	if err == nil {
		t.Skip("на этой машине конфиг лежит в одном из стандартных мест")
	}
	if !strings.Contains(err.Error(), "--config") || !strings.Contains(err.Error(), "laminara") {
		t.Fatalf("ошибка не подсказывает, где искали и что делать: %v", err)
	}
}
