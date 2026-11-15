package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"time"

	"lukechampine.com/blake3"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

type CAS struct {
	backend Backend
	algo    corev1.HashAlgo
}

func NewCAS(backend Backend, algo corev1.HashAlgo) *CAS {
	if algo == corev1.HashAlgo_HASH_ALGO_UNSPECIFIED {
		algo = corev1.HashAlgo_HASH_ALGO_BLAKE3
	}
	return &CAS{backend: backend, algo: algo}
}

func (c *CAS) Put(ctx context.Context, r io.Reader) (*corev1.ObjectRef, error) {
	hasher, err := newHasher(c.algo)
	if err != nil {
		return nil, err
	}
	staged, err := createStagingFile()
	if err != nil {
		return nil, err
	}
	defer staged.Close()

	size, err := io.Copy(io.MultiWriter(staged, hasher), contextReader{ctx: ctx, reader: r})
	if err != nil {
		return nil, err
	}
	sum := hasher.Sum(nil)
	key := ObjectKey(c.algo, sum)

	valid, err := c.existingMatches(ctx, key, sum, size)
	if err != nil {
		return nil, err
	}
	if !valid {
		if _, err := staged.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := c.backend.Put(ctx, key, contextFile{ctx: ctx, file: staged}, size); err != nil {
			return nil, err
		}
	}
	return objectRef(c.algo, sum, size), nil
}

type FileSource interface {
	PutFromFile(ctx context.Context, key, src string, verify func(io.Reader) error) error
}

var errObjectMismatch = errors.New("записанные байты не совпали с тем файлом, который хешировали")

func (c *CAS) PutFile(ctx context.Context, path string) (*corev1.ObjectRef, error) {
	sum, size, err := c.hashFile(ctx, path)
	if err != nil {
		return nil, err
	}
	key := ObjectKey(c.algo, sum)

	valid, err := c.existingMatches(ctx, key, sum, size)
	if err != nil {
		return nil, err
	}
	if !valid {
		if err := c.storeFile(ctx, key, path, sum, size); err != nil {
			return nil, err
		}
	}
	return objectRef(c.algo, sum, size), nil
}

func (c *CAS) storeFile(ctx context.Context, key, path string, sum []byte, size int64) error {
	source, ok := c.backend.(FileSource)
	if !ok {
		return c.streamFile(ctx, key, path, sum, size)
	}
	err := source.PutFromFile(ctx, key, path, c.matches(ctx, sum, size))
	if errors.Is(err, errObjectMismatch) {
		return changedWhilePublished(path)
	}
	return err
}

func (c *CAS) streamFile(ctx context.Context, key, path string, sum []byte, size int64) error {
	hasher, err := newHasher(c.algo)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	staged, err := createStagingFile()
	if err != nil {
		return err
	}
	defer staged.Close()
	written, err := io.Copy(io.MultiWriter(staged, hasher), contextReader{ctx: ctx, reader: file})
	if err != nil {
		return err
	}
	if written != size || !bytes.Equal(hasher.Sum(nil), sum) {
		return changedWhilePublished(path)
	}
	if _, err := staged.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return c.backend.Put(ctx, key, contextFile{ctx: ctx, file: staged}, size)
}

func changedWhilePublished(path string) error {
	return fmt.Errorf("файл %s изменился во время публикации — повторите публикацию", path)
}

func (c *CAS) matches(ctx context.Context, sum []byte, size int64) func(io.Reader) error {
	return func(r io.Reader) error {
		hasher, err := newHasher(c.algo)
		if err != nil {
			return err
		}
		written, err := io.Copy(hasher, contextReader{ctx: ctx, reader: r})
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if written != size || !bytes.Equal(hasher.Sum(nil), sum) {
			return errObjectMismatch
		}
		return nil
	}
}

func (c *CAS) hashFile(ctx context.Context, path string) ([]byte, int64, error) {
	hasher, err := newHasher(c.algo)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	size, err := io.Copy(hasher, contextReader{ctx: ctx, reader: file})
	file.Close()
	if err != nil {
		return nil, 0, err
	}
	return hasher.Sum(nil), size, nil
}

func (c *CAS) existingMatches(ctx context.Context, key string, sum []byte, size int64) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	storedSize, exists, err := c.backend.Stat(ctx, key)
	if err != nil || !exists {
		return false, err
	}
	if storedSize != size {
		return false, nil
	}
	reader, err := c.backend.Get(ctx, key)
	if err != nil {
		return false, fmt.Errorf("не удалось проверить объект %s: %w", key, err)
	}
	err = c.matches(ctx, sum, size)(reader)
	closeErr := reader.Close()
	if cancelled := ctx.Err(); cancelled != nil {
		return false, errors.Join(cancelled, closeErr)
	}
	if err != nil && !errors.Is(err, errObjectMismatch) {
		return false, fmt.Errorf("не удалось проверить объект %s: %w", key, errors.Join(err, closeErr))
	}
	if closeErr != nil {
		return false, fmt.Errorf("не удалось закрыть объект %s после проверки: %w", key, closeErr)
	}
	return err == nil, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(buffer []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(buffer)
}

type contextFile struct {
	ctx  context.Context
	file *os.File
}

func (f contextFile) Read(buffer []byte) (int, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.Read(buffer)
}

func (f contextFile) ReadAt(buffer []byte, offset int64) (int, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.ReadAt(buffer, offset)
}

func (f contextFile) Seek(offset int64, whence int) (int64, error) {
	if err := f.ctx.Err(); err != nil {
		return 0, err
	}
	return f.file.Seek(offset, whence)
}

func objectRef(algo corev1.HashAlgo, sum []byte, size int64) *corev1.ObjectRef {
	return &corev1.ObjectRef{
		Hash: &corev1.Hash{Algo: algo, Value: sum},
		Size: uint64(size),
	}
}

func (c *CAS) Get(ctx context.Context, ref *corev1.ObjectRef) (io.ReadCloser, error) {
	return c.backend.Get(ctx, ObjectKey(ref.Hash.Algo, ref.Hash.Value))
}

func (c *CAS) Locate(ctx context.Context, ref *corev1.ObjectRef, ttl time.Duration) (Location, error) {
	return c.backend.Locate(ctx, ObjectKey(ref.Hash.Algo, ref.Hash.Value), ttl)
}

func ObjectKey(algo corev1.HashAlgo, sum []byte) string {
	encoded := hex.EncodeToString(sum)
	return fmt.Sprintf("%s/%s/%s/%s", algoName(algo), encoded[0:2], encoded[2:4], encoded)
}

func algoName(algo corev1.HashAlgo) string {
	switch algo {
	case corev1.HashAlgo_HASH_ALGO_BLAKE3:
		return "blake3"
	case corev1.HashAlgo_HASH_ALGO_SHA256:
		return "sha256"
	case corev1.HashAlgo_HASH_ALGO_SHA1:
		return "sha1"
	default:
		return "unknown"
	}
}

func newHasher(algo corev1.HashAlgo) (hash.Hash, error) {
	switch algo {
	case corev1.HashAlgo_HASH_ALGO_BLAKE3:
		return blake3.New(32, nil), nil
	case corev1.HashAlgo_HASH_ALGO_SHA256:
		return sha256.New(), nil
	default:
		return nil, fmt.Errorf("хеш %v сервер не считает", algo)
	}
}
