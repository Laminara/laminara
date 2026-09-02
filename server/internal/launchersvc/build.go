package launchersvc

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/laminara/laminara/server/internal/bake"
	"github.com/laminara/laminara/server/internal/clientconfig"
	"github.com/laminara/laminara/server/internal/ghrelease"
	"github.com/laminara/laminara/server/internal/humanize"
	"github.com/laminara/laminara/server/internal/version"
)

const (
	linuxTemplate   = "laminara-launcher-linux-x86_64"
	windowsTemplate = "laminara-launcher-windows-x86_64.exe"
	templatesDir    = ".templates"
)

type Bakery struct {
	Repo     string
	Version  string
	Auto     bool
	Document func() (clientconfig.Document, error)
	API      string
	HTTP     *http.Client
}

func (s *Service) SetBakery(b *Bakery) {
	s.bakery = b
}

func (s *Service) build(ctx context.Context, args []string, out io.Writer) error {
	if s.bakery == nil || s.bakery.Document == nil {
		return fmt.Errorf("сборка лаунчера не настроена: нужны build.signingKeyPath и launcher.dir")
	}
	target := strings.TrimPrefix(strings.TrimSpace(s.bakery.Version), "v")
	if len(args) > 0 {
		target = strings.TrimPrefix(strings.TrimSpace(args[0]), "v")
	}
	if !version.IsValid(target) {
		return fmt.Errorf("версия %q не похожа на номер версии, например 1.2.3", target)
	}
	if err := s.roomFor(target); err != nil {
		return err
	}

	document, err := s.bakery.Document()
	if err != nil {
		return err
	}
	if len(document.Endpoints) == 0 {
		return fmt.Errorf("не задан адрес для игроков: впишите launcher.endpoints в конфиг — этот адрес запекается в лаунчер")
	}
	payload, err := document.JSON()
	if err != nil {
		return err
	}

	name := document.LauncherName()
	dir := filepath.Join(s.dir, target)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fmt.Fprintf(out, "Собираю лаунчер %s версии %s\n", name, target)

	for _, template := range []struct {
		asset  string
		suffix string
	}{
		{linuxTemplate, ""},
		{windowsTemplate, ".exe"},
	} {
		source, err := s.bakery.template(ctx, target, template.asset, s.dir, out)
		if err != nil {
			return err
		}
		image, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		baked, err := bake.Attach(image, payload)
		if err != nil {
			return err
		}
		if template.suffix == ".exe" {
			baked = brand(baked, document, out)
		}
		artifact := filepath.Join(dir, name+template.suffix)
		if err := os.WriteFile(artifact, baked, 0o755); err != nil {
			return err
		}
		fmt.Fprintf(out, "  %-28s %s\n", filepath.Base(artifact), humanize.Bytes(uint64(len(baked))))
	}

	return s.publish(ctx, target, out)
}

func (s *Service) Catch(ctx context.Context, log *slog.Logger) {
	if s == nil || s.bakery == nil || !s.bakery.Auto || s.bakery.Document == nil {
		return
	}
	published, _, err := NewReleases(s.dir).Current()
	if err != nil || len(published) == 0 {
		return
	}
	decoded, err := Decode(published)
	if err != nil {
		return
	}
	target := strings.TrimPrefix(s.bakery.Version, "v")
	if !version.IsNewer(target, decoded.Version) {
		return
	}

	log.Info("сервер обновился — пересобираю лаунчер", "source", "launcher", "было", decoded.Version, "стало", target)
	var out strings.Builder
	if err := s.build(ctx, []string{target}, &out); err != nil {
		log.Error("лаунчер пересобрать не вышло", "source", "launcher", "error", err, "вывод", out.String())
		return
	}
	log.Info("лаунчер пересобран и выпущен", "source", "launcher", "версия", target)
}

func (s *Service) roomFor(candidate string) error {
	current, _, err := NewReleases(s.dir).Current()
	if err != nil {
		return err
	}
	if len(current) == 0 {
		return nil
	}
	decoded, err := Decode(current)
	if err != nil {
		return err
	}
	if version.IsNewer(candidate, decoded.Version) {
		return nil
	}
	return fmt.Errorf("версия %s уже опубликована — назовите новую: launcher build %s", decoded.Version, version.NextPatch(decoded.Version))
}

func (b *Bakery) template(ctx context.Context, tag, asset, root string, out io.Writer) (string, error) {
	dir := filepath.Join(root, templatesDir, tag)
	path := filepath.Join(dir, asset)
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	releases := &ghrelease.Client{Repo: b.Repo, API: b.API, HTTP: b.HTTP}
	release, err := releases.ByTag(ctx, "v"+tag)
	if err != nil {
		return "", fmt.Errorf("не нашёл релиз v%s, из которого берут лаунчер: %w", tag, err)
	}
	if !release.Has(asset) {
		return "", fmt.Errorf("в релизе %s нет файла %s — в этой версии лаунчер не собирали", release.Tag, asset)
	}
	fmt.Fprintf(out, "  скачиваю %s\n", asset)
	if err := releases.Download(ctx, release, asset, path); err != nil {
		return "", err
	}
	return path, nil
}
