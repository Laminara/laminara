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
	staged, err := os.CreateTemp("", "laminara-cas-*")
	if err != nil {
		return nil, err
	}
	defer os.Remove(staged.Name())
	defer staged.Close()

	size, err := io.Copy(io.MultiWriter(staged, hasher), r)
	if err != nil {
		return nil, err
	}
	sum := hasher.Sum(nil)
	key := ObjectKey(c.algo, sum)

	exists, err := c.exists(ctx, key)
	if err != nil {
		return nil, err
	}
	if !exists {
		if _, err := staged.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := c.backend.Put(ctx, key, staged, size); err != nil {
			return nil, err
		}
	}
	return objectRef(c.algo, sum, size), nil
}

type FileSource interface {
	PutFromFile(ctx context.Context, key, src string, verify func(io.Reader) error) error
}

var errObjectMismatch = errors.New("the stored bytes do not match the file that was hashed")

func (c *CAS) PutFile(ctx context.Context, path string) (*corev1.ObjectRef, error) {
	sum, size, err := c.hashFile(path)
	if err != nil {
		return nil, err
	}
	key := ObjectKey(c.algo, sum)

	exists, err := c.exists(ctx, key)
	if err != nil {
		return nil, err
	}
	if !exists {
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
	err := source.PutFromFile(ctx, key, path, c.matches(sum, size))
	if errors.Is(err, errObjectMismatch) {
		return changedWhilePublished(path, nil)
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
	err = c.backend.Put(ctx, key, io.TeeReader(file, hasher), size)
	file.Close()
	if err != nil {
		return err
	}
	if bytes.Equal(hasher.Sum(nil), sum) {
		return nil
	}
	return changedWhilePublished(path, c.backend.Delete(ctx, key))
}

func changedWhilePublished(path string, cleanup error) error {
	err := fmt.Errorf("%s changed while it was being published", path)
	if cleanup == nil {
		return err
	}
	return errors.Join(err, fmt.Errorf("the object it produced is still in the store and has to be removed by hand: %w", cleanup))
}

func (c *CAS) matches(sum []byte, size int64) func(io.Reader) error {
	return func(r io.Reader) error {
		hasher, err := newHasher(c.algo)
		if err != nil {
			return err
		}
		written, err := io.Copy(hasher, r)
		if err != nil {
			return err
		}
		if written != size || !bytes.Equal(hasher.Sum(nil), sum) {
			return errObjectMismatch
		}
		return nil
	}
}

func (c *CAS) hashFile(path string) ([]byte, int64, error) {
	hasher, err := newHasher(c.algo)
	if err != nil {
		return nil, 0, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	size, err := io.Copy(hasher, file)
	file.Close()
	if err != nil {
		return nil, 0, err
	}
	return hasher.Sum(nil), size, nil
}

func (c *CAS) exists(ctx context.Context, key string) (bool, error) {
	_, exists, err := c.backend.Stat(ctx, key)
	return exists, err
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
		return nil, fmt.Errorf("unsupported hash algorithm %v", algo)
	}
}
