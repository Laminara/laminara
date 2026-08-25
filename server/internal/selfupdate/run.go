package selfupdate

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

func (c *Checker) Apply(ctx context.Context, release *Release) error {
	binary, err := BinaryPath()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(binary), ".laminara-update-")
	if err != nil {
		return fmt.Errorf("не удалось подготовить загрузку рядом с %s: %w", binary, err)
	}
	defer os.RemoveAll(dir)

	staged, err := c.Download(ctx, release, dir)
	if err != nil {
		return err
	}
	return Install(staged, binary)
}

func Run(ctx context.Context, checker *Checker, current string, checkOnly bool, restartHint string, out io.Writer) error {
	release, err := checker.Latest(ctx)
	if err != nil {
		return err
	}

	if !release.IsNewerThan(current) {
		fmt.Fprintf(out, "Установлена версия %s, свежее пока нет\n", current)
		return nil
	}

	fmt.Fprintf(out, "Есть версия %s, установлена %s\n", release.Version, current)
	if release.Notes != "" {
		fmt.Fprintf(out, "\n%s\n\n", release.Notes)
	}
	if checkOnly {
		return nil
	}

	if err := checker.Apply(ctx, release); err != nil {
		return err
	}
	fmt.Fprintf(out, "Версия %s записана. Она заработает после перезапуска: %s\n", release.Version, restartHint)
	return nil
}
