package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/laminara/laminara/server/internal/humanize"
)

const notesLines = 8

func (c *Checker) Apply(ctx context.Context, release *Release) error {
	binary, err := BinaryPath()
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp(filepath.Dir(binary), ".laminara-update-")
	if err != nil {
		if errors.Is(err, syscall.EROFS) {
			return fmt.Errorf("каталог %s только для чтения — самообновление отключено защитой systemd. Обновите переустановкой (curl -fsSL https://raw.githubusercontent.com/laminara/laminara/main/install.sh | bash) или добавьте каталог бинаря в ReadWritePaths юнита", filepath.Dir(binary))
		}
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
	if notes := shorten(release.Notes); notes != "" {
		fmt.Fprintf(out, "\n%s\n", notes)
		if release.URL != "" {
			fmt.Fprintf(out, "Целиком: %s\n", release.URL)
		}
		fmt.Fprintln(out)
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

func shorten(notes string) string {
	lines := strings.Split(strings.TrimSpace(notes), "\n")
	if len(lines) <= notesLines {
		return strings.Join(lines, "\n")
	}
	kept := append([]string{}, lines[:notesLines]...)
	kept = append(kept, "…и ещё "+humanize.Count(len(lines)-notesLines, "строка", "строки", "строк"))
	return strings.Join(kept, "\n")
}
