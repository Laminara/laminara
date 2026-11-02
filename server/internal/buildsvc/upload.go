package buildsvc

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	adminv1 "github.com/laminara/laminara/gen/go/laminara/admin/v1"
	"github.com/laminara/laminara/server/internal/buildspec"
)

func (s *Service) uploadDir() string { return filepath.Join(s.profilesDir, ".laminara-uploads") }

func (s *Service) UploadBuildChunk(ctx context.Context, request *adminv1.UploadBuildChunkRequest) (*adminv1.UploadBuildChunkResponse, error) {
	if !buildspec.ValidHash(request.Sha256) || request.Size > buildspec.MaxFileSize || len(request.Data) > 1<<20 || request.Offset > request.Size || uint64(len(request.Data)) > request.Size-request.Offset {
		return nil, errors.New("неверный хеш, размер или смещение загрузки")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	unlock, err := s.alone(".upload")
	if err != nil {
		return nil, err
	}
	defer unlock()
	dir := s.uploadDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	if info, err := root.Stat(request.Sha256); err == nil && info.Size() == int64(request.Size) {
		return &adminv1.UploadBuildChunkResponse{Offset: request.Size, Complete: true}, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var used int64
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if !info.Mode().IsRegular() {
			return nil, errors.New("неверный объект в каталоге загрузок")
		}
		if time.Since(info.ModTime()) > 24*time.Hour {
			if err := root.Remove(entry.Name()); err != nil {
				return nil, err
			}
			continue
		}
		used += info.Size()
	}
	if used+int64(len(request.Data)) > 16<<30 {
		return nil, errors.New("кэш загрузок заполнен; повторите после очистки устаревших загрузок")
	}
	part := request.Sha256 + ".part"
	file, err := root.OpenFile(part, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if uint64(info.Size()) != request.Offset {
		return &adminv1.UploadBuildChunkResponse{Offset: uint64(info.Size())}, nil
	}
	if _, err := file.WriteAt(request.Data, int64(request.Offset)); err != nil {
		return nil, err
	}
	offset := request.Offset + uint64(len(request.Data))
	if err := file.Sync(); err != nil {
		return nil, err
	}
	if offset < request.Size {
		return &adminv1.UploadBuildChunkResponse{Offset: offset}, nil
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", hasher.Sum(nil)) != request.Sha256 {
		file.Close()
		_ = root.Remove(part)
		return nil, errors.New("хеш загруженного файла не совпал")
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	if err := root.Rename(part, request.Sha256); err != nil {
		return nil, err
	}
	return &adminv1.UploadBuildChunkResponse{Offset: offset, Complete: true}, nil
}
