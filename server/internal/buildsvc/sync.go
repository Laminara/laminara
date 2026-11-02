package buildsvc

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/atomicfile"
	"github.com/laminara/laminara/server/internal/buildspec"
	"github.com/laminara/laminara/server/internal/catalog"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/prepare"
)

func (s *Service) profileRoot(name string) string {
	root := filepath.Join(s.profilesDir, name)
	pointer, err := catalog.ReadRelease(s.profilesDir, name)
	if os.IsNotExist(err) {
		return root
	}
	if err != nil {
		return filepath.Join(root, ".invalid-release")
	}
	if pointer.Workspace != "" {
		return filepath.Join(root, ".laminara", "workspaces", pointer.Workspace)
	}
	return root
}

func workspaceDigest(ctx context.Context, root string) (string, error) {
	hash := sha256.New()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".laminara" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return errors.New("сборка содержит ссылку или специальный файл")
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", filepath.ToSlash(rel), info.Size())
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(hash, file)
		closeErr := file.Close()
		return errors.Join(copyErr, closeErr)
	})
	if os.IsNotExist(err) {
		return "absent", nil
	}
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *Service) buildRevision(ctx context.Context, name string) (string, string, error) {
	workspace, err := workspaceDigest(ctx, s.profileRoot(name))
	if err != nil {
		return "", "", err
	}
	pointer, err := catalog.ReadRelease(s.profilesDir, name)
	if err != nil && !os.IsNotExist(err) {
		return "", "", err
	}
	current := ""
	if pointer != nil {
		current = pointer.Current
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(current+":"+workspace))), workspace, nil
}

func (s *Service) PlanBuild(ctx context.Context, data []byte) (*adminv1.PlanBuildResponse, error) {
	spec, err := buildspec.Decode(data)
	if err != nil {
		return nil, err
	}
	if !safeName(spec.Name) {
		return nil, errors.New("недопустимое имя сборки")
	}
	unlock, err := s.alone(spec.Name)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.planBuild(ctx, spec)
}

func (s *Service) planBuild(ctx context.Context, spec *buildspec.Spec) (*adminv1.PlanBuildResponse, error) {
	_, digest, err := spec.Canonical()
	if err != nil {
		return nil, err
	}
	revision, workspace, err := s.buildRevision(ctx, spec.Name)
	if err != nil {
		return nil, err
	}
	result := &adminv1.PlanBuildResponse{Revision: revision, InputDigest: digest}
	if pointer, err := catalog.ReadRelease(s.profilesDir, spec.Name); err == nil && pointer.InputDigest == digest && pointer.WorkspaceDigest == workspace {
		result.NoOp = true
		return result, nil
	}
	seen := map[string]bool{}
	for _, file := range spec.Files {
		if file.Source != "" || !buildspec.ValidHash(file.SHA256) {
			return nil, errors.New("сервер принимает файлы по хешу, без локального source")
		}
		if info, err := os.Lstat(filepath.Join(s.uploadDir(), file.SHA256)); err != nil || !info.Mode().IsRegular() || info.Size() != int64(file.Size) {
			if !seen[file.SHA256] {
				result.MissingHashes = append(result.MissingHashes, file.SHA256)
				seen[file.SHA256] = true
			}
		}
	}
	return result, nil
}

func (s *Service) applyBuild(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 {
		return errors.New("build-apply требует описание JSON и ревизию из PlanBuild")
	}
	spec, err := buildspec.Decode([]byte(args[0]))
	if err != nil {
		return err
	}
	if !safeName(spec.Name) {
		return errors.New("недопустимое имя сборки")
	}
	unlock, err := s.alone(spec.Name)
	if err != nil {
		return err
	}
	defer unlock()
	plan, err := s.planBuild(ctx, spec)
	if err != nil {
		return err
	}
	if plan.NoOp {
		fmt.Fprintln(out, "Состояние уже применено:", plan.InputDigest)
		return nil
	}
	if plan.Revision != args[1] {
		return errors.New("сборка изменилась после plan; получите новый план")
	}
	if len(plan.MissingHashes) > 0 {
		return errors.New("сначала загрузите все файлы из плана")
	}
	staging := filepath.Join(s.profilesDir, ".laminara-staging")
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(staging, "apply-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	worker := NewService(s.cas, s.signer, stage)
	worker.runtimeVersions = spec.RuntimeVersions
	worker.mojang = s.mojang
	worker.preparer = s.preparer
	compat := spec.Compat
	if compat == "off" {
		compat = "нет"
	}
	install := []string{spec.Name, spec.Minecraft, "loader=" + spec.Loader, "compat=" + compat, "platform=" + strings.Join(spec.Platforms, ",")}
	if spec.LoaderVersion != "" {
		install = append(install, "loaderVersion="+spec.LoaderVersion)
	}
	if spec.Java != "" {
		install = append(install, "java="+spec.Java)
	}
	if err := worker.prepare(ctx, install, out); err != nil {
		return err
	}
	root := filepath.Join(stage, spec.Name)
	for _, file := range spec.Files {
		if err := s.copyUploaded(ctx, root, file); err != nil {
			return err
		}
	}
	settingsPath := filepath.Join(root, manifest.SettingsFileName)
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return err
	}
	var configured map[string]json.RawMessage
	if err := json.Unmarshal(data, &configured); err != nil {
		return err
	}
	for key, value := range spec.Settings {
		configured[key] = value
	}
	data, err = json.MarshalIndent(configured, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicfile.Write(settingsPath, data, 0o644); err != nil {
		return err
	}
	doc, err := worker.settingsDoc(spec.Name)
	if err != nil {
		return err
	}
	if err := doc.Validate(); err != nil {
		return err
	}
	layout := worker.layout(spec.Name)
	var files []catalog.ReleaseFile
	for _, variant := range layout.platforms {
		place := layout.place(variant)
		result, err := prepare.PublishPlatform(ctx, s.cas, s.signer, manifest.Sources{Shared: place.shared, Platform: place.platform}, root, spec.Name, "1", variant)
		if err != nil {
			return err
		}
		files = append(files, catalog.ReleaseFile{Name: catalog.ManifestName(spec.Name, variant), Canonical: result.Canonical, Signature: result.Signature})
	}
	if len(files) != len(spec.Platforms) {
		return errors.New("не все запрошенные платформы подготовлены")
	}
	fingerprint, err := workspaceDigest(ctx, root)
	if err != nil {
		return err
	}
	current, _, err := s.buildRevision(ctx, spec.Name)
	if err != nil {
		return err
	}
	if current != plan.Revision {
		return errors.New("файлы сборки изменились во время apply; публикация отменена")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	id := rand.Text()
	destination := filepath.Join(s.profilesDir, spec.Name, ".laminara", "workspaces", id)
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	if err := os.Rename(root, destination); err != nil {
		return err
	}
	metadata := catalog.ReleasePointer{Workspace: id, InputDigest: plan.InputDigest, SourceRevision: spec.SourceRevision, WorkspaceDigest: fingerprint, SettingsRevision: doc.Revision()}
	if err := catalog.PublishReleaseWithMetadata(s.profilesDir, spec.Name, files, metadata); err != nil {
		return err
	}
	if s.published != nil {
		s.published.Refresh()
	}
	s.fire("build.published", spec.Name)
	return json.NewEncoder(out).Encode(map[string]string{"build": spec.Name, "inputDigest": plan.InputDigest, "sourceRevision": spec.SourceRevision})
}

func (s *Service) copyUploaded(ctx context.Context, root string, entry buildspec.File) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	uploads, err := os.OpenRoot(s.uploadDir())
	if err != nil {
		return err
	}
	defer uploads.Close()
	source, err := uploads.Open(entry.SHA256)
	if err != nil {
		return err
	}
	defer source.Close()
	destination := filepath.Join(root, filepath.FromSlash(entry.Path))
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("файл перекрывает результат install: %s: %w", entry.Path, err)
	}
	hash := sha256.New()
	size, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(source, int64(entry.Size)+1))
	closeErr := file.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return err
	}
	if uint64(size) != entry.Size || fmt.Sprintf("%x", hash.Sum(nil)) != entry.SHA256 {
		return errors.New("загруженный файл повреждён")
	}
	return nil
}

func (s *Service) rollbackBuild(ctx context.Context, args []string, out io.Writer) error {
	if len(args) != 2 || !safeName(args[0]) {
		return errors.New("build-rollback <сборка> <идентификатор публикации>")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock, err := s.alone(args[0])
	if err != nil {
		return err
	}
	defer unlock()
	if err := catalog.RollbackRelease(s.profilesDir, args[0], args[1]); err != nil {
		return err
	}
	if s.published != nil {
		s.published.Refresh()
	}
	fmt.Fprintln(out, "Публикация восстановлена:", args[1])
	return nil
}
