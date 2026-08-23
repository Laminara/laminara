package prepare

import (
	"context"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
	"github.com/laminara/laminara/server/internal/manifest"
	"github.com/laminara/laminara/server/internal/progress"
	"github.com/laminara/laminara/server/internal/storage"
)

type Published struct {
	Manifest  *corev1.Manifest
	Canonical []byte
	Signature []byte
}

func Publish(ctx context.Context, cas *storage.CAS, signer *manifest.Signer, profileDir, name, version string) (*Published, error) {
	return PublishVariant(ctx, cas, signer, profileDir, profileDir, name, version, corev1.Platform_PLATFORM_UNSPECIFIED)
}

func PublishVariant(
	ctx context.Context,
	cas *storage.CAS,
	signer *manifest.Signer,
	profileDir, settingsRoot, name, version string,
	platform corev1.Platform,
) (*Published, error) {
	built, err := manifest.NewBuilder(cas).BuildVariant(ctx, profileDir, settingsRoot, name, version, platform)
	if err != nil {
		return nil, err
	}
	progress.Phase(ctx, "Подпись манифеста")
	canonical, signature, err := signer.Sign(built)
	if err != nil {
		return nil, err
	}
	return &Published{Manifest: built, Canonical: canonical, Signature: signature}, nil
}
