package hash

import (
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
)

type Verifier interface {
	Verify(password, stored string) (bool, error)
}

type Hasher interface {
	Hash(password string) (string, error)
}

func Produce(scheme, password string) (string, error) {
	verifier, err := Get(scheme)
	if err != nil {
		return "", err
	}
	hasher, ok := verifier.(Hasher)
	if !ok {
		return "", fmt.Errorf("hash scheme %q cannot produce hashes", scheme)
	}
	return hasher.Hash(password)
}

func hexOf(sum []byte) string {
	return hex.EncodeToString(sum)
}

var registry = map[string]Verifier{}

func Register(name string, verifier Verifier) {
	registry[name] = verifier
}

func Get(name string) (Verifier, error) {
	verifier, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("unknown hash scheme %q", name)
	}
	return verifier, nil
}

var weak = map[string]string{
	"plain":  "пароли лежат открытым текстом",
	"md5":    "MD5 подбирается мгновенно",
	"sha256": "SHA-256 без соли перебирается на видеокарте",
	"sha512": "SHA-512 без соли перебирается на видеокарте",
}

func Weakness(scheme string) (string, bool) {
	reason, found := weak[scheme]
	return reason, found
}

func Names() []string {
	names := make([]string, 0, len(registry))
	for name := range registry {
		names = append(names, name)
	}
	return names
}

func constantTimeHexEqual(sum []byte, stored string) bool {
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum)), []byte(strings.ToLower(stored))) == 1
}
