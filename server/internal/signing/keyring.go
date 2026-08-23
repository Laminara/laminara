package signing

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"strings"
)

type Keyring struct {
	active  ed25519.PrivateKey
	trusted []ed25519.PublicKey
}

func NewKeyring(activePath string, additional []string) (*Keyring, error) {
	active, err := LoadOrCreate(activePath)
	if err != nil {
		return nil, err
	}
	ring := &Keyring{active: active}
	ring.trusted = append(ring.trusted, active.Public().(ed25519.PublicKey))

	for _, entry := range additional {
		public, err := parseTrusted(entry)
		if err != nil {
			return nil, err
		}
		if ring.Trusts(public) {
			continue
		}
		ring.trusted = append(ring.trusted, public)
	}
	return ring, nil
}

func parseTrusted(entry string) (ed25519.PublicKey, error) {
	trimmed := strings.TrimSpace(entry)
	if raw, err := hex.DecodeString(trimmed); err == nil && len(raw) == ed25519.PublicKeySize {
		return ed25519.PublicKey(raw), nil
	}
	private, err := Load(trimmed)
	if err != nil {
		return nil, fmt.Errorf("trusted signing key %q is neither a %d-byte public key in hex nor a readable key file: %w", entry, ed25519.PublicKeySize, err)
	}
	return private.Public().(ed25519.PublicKey), nil
}

func (k *Keyring) Active() ed25519.PrivateKey { return k.active }

func (k *Keyring) ActiveHex() string { return PublicKeyHex(k.active) }

func (k *Keyring) Trusts(public ed25519.PublicKey) bool {
	for _, known := range k.trusted {
		if known.Equal(public) {
			return true
		}
	}
	return false
}

func (k *Keyring) TrustedHex() []string {
	out := make([]string, 0, len(k.trusted))
	for _, public := range k.trusted {
		out = append(out, hex.EncodeToString(public))
	}
	return out
}
