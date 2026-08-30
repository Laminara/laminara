package signing_test

import (
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/signing"
)

func TestLoadOrCreatePersistsAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.hex")

	first, err := signing.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := signing.LoadOrCreate(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatal("reloaded key differs from the created key")
	}

	message := []byte("laminara")
	sig := ed25519.Sign(second, message)
	if !ed25519.Verify(first.Public().(ed25519.PublicKey), message, sig) {
		t.Fatal("signature from reloaded key does not verify against original public key")
	}
}

func TestEmptyPathIsRefused(t *testing.T) {
	if _, err := signing.LoadOrCreate(""); !errors.Is(err, signing.ErrNoKeyPath) {
		t.Fatalf("LoadOrCreate(\"\") = %v, want ErrNoKeyPath: an ephemeral key would sign manifests that no launcher accepts after the next restart", err)
	}
	if _, err := signing.Load(""); !errors.Is(err, signing.ErrNoKeyPath) {
		t.Fatalf("Load(\"\") = %v, want ErrNoKeyPath", err)
	}
}
