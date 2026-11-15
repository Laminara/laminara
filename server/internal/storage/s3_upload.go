package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/minio/minio-go/v7"
	"golang.org/x/sync/errgroup"
)

const (
	s3PartSize       = 16 << 20
	s3UploadWorkers  = 4
	s3CleanupTimeout = 10 * time.Second
)

func (b *s3Backend) Put(ctx context.Context, key string, reader io.Reader, size int64) error {
	at, randomAccess := reader.(io.ReaderAt)
	seeker, seekable := reader.(io.Seeker)
	var offset int64
	if size < 0 || (size > s3PartSize && (!randomAccess || !seekable)) {
		staged, err := createStagingFile()
		if err != nil {
			return err
		}
		defer staged.Close()
		source := reader
		if size >= 0 {
			source = io.LimitReader(reader, size)
		}
		written, err := io.Copy(staged, contextReader{ctx: ctx, reader: source})
		if err != nil {
			return err
		}
		if size >= 0 && written != size {
			return fmt.Errorf("не удалось прочитать объект %s целиком: %d из %d байт", key, written, size)
		}
		if _, err := staged.Seek(0, io.SeekStart); err != nil {
			return err
		}
		file := contextFile{ctx: ctx, file: staged}
		reader, at, seeker = file, file, file
		size = written
	}
	if size <= s3PartSize {
		_, err := b.client.PutObject(ctx, b.bucket, key, reader, size, minio.PutObjectOptions{DisableMultipart: true})
		return err
	}
	var err error
	offset, err = seeker.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}
	if offset < 0 || offset > math.MaxInt64-size {
		return fmt.Errorf("неверная позиция чтения объекта %s: %d", key, offset)
	}
	return b.putMultipart(ctx, key, at, offset, size)
}

func (b *s3Backend) putMultipart(ctx context.Context, key string, reader io.ReaderAt, offset, size int64) (err error) {
	count, partSize, lastSize, err := minio.OptimalPartInfo(size, 0)
	if err != nil {
		return err
	}
	core := minio.Core{Client: b.client}
	uploadID, err := core.NewMultipartUpload(ctx, b.bucket, key, minio.PutObjectOptions{})
	if uploadID == "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("S3 не вернул идентификатор загрузки объекта %s", key)
	}
	completed := false
	defer func() {
		if completed {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), s3CleanupTimeout)
		defer cancel()
		abort := core.AbortMultipartUpload(cleanup, b.bucket, key, uploadID)
		if abort != nil && minio.ToErrorResponse(abort).Code != "NoSuchUpload" {
			err = errors.Join(err, fmt.Errorf("не удалось убрать незавершённую загрузку объекта %s (uploadID=%s): %w", key, uploadID, abort))
		}
	}()
	if err != nil {
		return err
	}
	parts := make([]minio.CompletePart, count)
	group, uploading := errgroup.WithContext(ctx)
	group.SetLimit(s3UploadWorkers)
	for index := range count {
		if uploading.Err() != nil {
			break
		}
		group.Go(func() error {
			length := partSize
			if index == count-1 {
				length = lastSize
			}
			section := io.NewSectionReader(reader, offset+int64(index)*partSize, length)
			digest := sha256.New()
			read, err := io.Copy(digest, contextReader{ctx: uploading, reader: section})
			if err != nil {
				return err
			}
			if read != length {
				return fmt.Errorf("не удалось прочитать часть %d объекта %s целиком: %d из %d байт", index+1, key, read, length)
			}
			if _, err := section.Seek(0, io.SeekStart); err != nil {
				return err
			}
			part, err := core.PutObjectPart(uploading, b.bucket, key, uploadID, index+1, section, length, minio.PutObjectPartOptions{
				Sha256Hex: hex.EncodeToString(digest.Sum(nil)), DisableContentSha256: true,
			})
			if err != nil {
				return err
			}
			parts[index] = minio.CompletePart{PartNumber: index + 1, ETag: part.ETag}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	_, err = core.CompleteMultipartUpload(ctx, b.bucket, key, uploadID, parts, minio.PutObjectOptions{})
	if err != nil {
		return err
	}
	completed = true
	return nil
}
