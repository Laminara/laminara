package manifest

import (
	"context"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/storage"
)

func GC(ctx context.Context, backend storage.Backend, live []*corev1.Manifest) (int, error) {
	referenced := make(map[string]struct{})
	for _, m := range live {
		for _, file := range m.Files {
			referenced[storage.ObjectKey(file.Object.Hash.Algo, file.Object.Hash.Value)] = struct{}{}
		}
	}
	keys, err := backend.List(ctx, "")
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, key := range keys {
		if _, ok := referenced[key]; ok {
			continue
		}
		if err := backend.Delete(ctx, key); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}
