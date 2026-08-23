package signing_test

import (
	"crypto/ed25519"
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

func TestEmptyPathGeneratesEphemeral(t *testing.T) {
	key, err := signing.LoadOrCreate("")
	if err != nil {
		t.Fatal(err)
	}
	if len(key) != ed25519.PrivateKeySize {
		t.Fatalf("key size = %d", len(key))
	}
}
