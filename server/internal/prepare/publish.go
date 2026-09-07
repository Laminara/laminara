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

func PublishVariant(
	ctx context.Context,
	cas *storage.CAS,
	signer *manifest.Signer,
	profileDir, settingsRoot, name, version string,
	platform corev1.Platform,
) (*Published, error) {
	return PublishPlatform(ctx, cas, signer, manifest.Sources{Shared: profileDir, Platform: profileDir}, settingsRoot, name, version, platform)
}

func PublishPlatform(
	ctx context.Context,
	cas *storage.CAS,
	signer *manifest.Signer,
	sources manifest.Sources,
	settingsRoot, name, version string,
	platform corev1.Platform,
) (*Published, error) {
	built, err := manifest.NewBuilder(cas).BuildPlatform(ctx, sources, settingsRoot, name, version, platform)
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
