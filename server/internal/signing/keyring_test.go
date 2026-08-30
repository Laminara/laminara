package signing_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/signing"
)

func writeKey(t *testing.T, dir, name string) (string, ed25519.PublicKey) {
	t.Helper()
	path := filepath.Join(dir, name)
	key, err := signing.Generate(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, key.Public().(ed25519.PublicKey)
}

func TestActiveKeyIsAlwaysTrusted(t *testing.T) {
	dir := t.TempDir()

	ring, err := signing.NewKeyring(filepath.Join(dir, "active.hex"), nil)
	if err != nil {
		t.Fatal(err)
	}

	active := ring.Active().Public().(ed25519.PublicKey)
	if !ring.Trusts(active) {
		t.Fatal("keyring does not trust its own active key")
	}
	trusted := ring.TrustedHex()
	if len(trusted) != 1 || trusted[0] != ring.ActiveHex() {
		t.Fatalf("trusted = %v, want only the active key %s", trusted, ring.ActiveHex())
	}
}

func TestRetiredKeysStayTrusted(t *testing.T) {
	dir := t.TempDir()
	retiredPath, retired := writeKey(t, dir, "retired.hex")
	_, foreign := writeKey(t, dir, "foreign.hex")

	ring, err := signing.NewKeyring(filepath.Join(dir, "active.hex"), []string{
		retiredPath,
		hex.EncodeToString(foreign),
	})
	if err != nil {
		t.Fatal(err)
	}

	if !ring.Trusts(retired) {
		t.Fatal("a retired key given as a file path is not trusted; launchers baked with it would stop accepting manifests")
	}
	if !ring.Trusts(foreign) {
		t.Fatal("a key given as public hex is not trusted")
	}
	if len(ring.TrustedHex()) != 3 {
		t.Fatalf("trusted = %d, want 3", len(ring.TrustedHex()))
	}
}

func TestKeyListedTwiceIsStoredOnce(t *testing.T) {
	dir := t.TempDir()
	activePath := filepath.Join(dir, "active.hex")

	ring, err := signing.NewKeyring(activePath, nil)
	if err != nil {
		t.Fatal(err)
	}
	repeated, err := signing.NewKeyring(activePath, []string{activePath, ring.ActiveHex()})
	if err != nil {
		t.Fatal(err)
	}

	if len(repeated.TrustedHex()) != 1 {
		t.Fatalf("trusted = %v, want the active key once", repeated.TrustedHex())
	}
}

func TestUnknownKeyIsNotTrusted(t *testing.T) {
	dir := t.TempDir()
	ring, err := signing.NewKeyring(filepath.Join(dir, "active.hex"), nil)
	if err != nil {
		t.Fatal(err)
	}

	stranger, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if ring.Trusts(stranger) {
		t.Fatal("keyring trusts a key it has never seen")
	}
}

func TestKeyringWithoutAPathIsRefused(t *testing.T) {
	if _, err := signing.NewKeyring("", nil); err == nil {
		t.Fatal("an empty key path must stop the setup instead of inventing a key nobody can trust tomorrow")
	}
}

func TestUnreadableEntryFails(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.hex")
	if err := os.WriteFile(broken, []byte("not a key\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, entry := range []string{"zzzz", broken, filepath.Join(dir, "absent.hex")} {
		if _, err := signing.NewKeyring(filepath.Join(dir, "active.hex"), []string{entry}); err == nil {
			t.Errorf("entry %q was accepted; a mistyped trusted key must stop the server, not silently drop trust", entry)
		}
	}
}

func TestGenerateNeverOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active.hex")

	first, err := signing.Generate(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signing.Generate(path); err == nil {
		t.Fatal("Generate overwrote an existing key")
	}

	reloaded, err := signing.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(reloaded) {
		t.Fatal("key on disk changed after the refused rotation")
	}
}
