package manifest

import (
	"crypto/ed25519"
	"sort"

	"google.golang.org/protobuf/proto"

	corev1 "github.com/laminara/laminara/gen/go/laminara/core/v1"
)

func Canonical(m *corev1.Manifest) ([]byte, error) {
	clone := proto.Clone(m).(*corev1.Manifest)
	sort.Slice(clone.Files, func(i, j int) bool {
		return clone.Files[i].Path < clone.Files[j].Path
	})
	return proto.MarshalOptions{Deterministic: true}.Marshal(clone)
}

type Signer struct {
	key ed25519.PrivateKey
}

func NewSigner(key ed25519.PrivateKey) *Signer {
	return &Signer{key: key}
}

func (s *Signer) Sign(m *corev1.Manifest) (canonical, signature []byte, err error) {
	canonical, err = Canonical(m)
	if err != nil {
		return nil, nil, err
	}
	return canonical, ed25519.Sign(s.key, canonical), nil
}

func CanonicalMessage(m proto.Message) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(m)
}

func (s *Signer) SignMessage(m proto.Message) (canonical, signature []byte, err error) {
	canonical, err = CanonicalMessage(m)
	if err != nil {
		return nil, nil, err
	}
	return canonical, ed25519.Sign(s.key, canonical), nil
}

func Verify(pub ed25519.PublicKey, canonical, signature []byte) bool {
	return ed25519.Verify(pub, canonical, signature)
}
