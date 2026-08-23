package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	_, exists, err := c.backend.Stat(ctx, key)
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
	return &corev1.ObjectRef{
		Hash: &corev1.Hash{Algo: c.algo, Value: sum},
		Size: uint64(size),
	}, nil
}

func (c *CAS) Get(ctx context.Context, ref *corev1.ObjectRef) (io.ReadCloser, error) {
	return c.backend.Get(ctx, ObjectKey(ref.Hash.Algo, ref.Hash.Value))
}

func (c *CAS) Has(ctx context.Context, ref *corev1.ObjectRef) (bool, error) {
	_, exists, err := c.backend.Stat(ctx, ObjectKey(ref.Hash.Algo, ref.Hash.Value))
	return exists, err
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
