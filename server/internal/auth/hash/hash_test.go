package hash_test

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/alexedwards/argon2id"
	"golang.org/x/crypto/bcrypt"

	"github.com/laminara/laminara/server/internal/auth/hash"
)

func verify(t *testing.T, scheme, password, stored string) bool {
	t.Helper()
	verifier, err := hash.Get(scheme)
	if err != nil {
		t.Fatalf("get %s: %v", scheme, err)
	}
	ok, err := verifier.Verify(password, stored)
	if err != nil {
		t.Fatalf("verify %s: %v", scheme, err)
	}
	return ok
}

func TestSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte("secret"))
	stored := hex.EncodeToString(sum[:])
	if !verify(t, "sha256", "secret", stored) {
		t.Fatal("correct password rejected")
	}
	if verify(t, "sha256", "wrong", stored) {
		t.Fatal("wrong password accepted")
	}
}

func TestArgon2id(t *testing.T) {
	stored, err := argon2id.CreateHash("secret", argon2id.DefaultParams)
	if err != nil {
		t.Fatal(err)
	}
	if !verify(t, "argon2id", "secret", stored) {
		t.Fatal("correct password rejected")
	}
	if verify(t, "argon2id", "wrong", stored) {
		t.Fatal("wrong password accepted")
	}
}

func TestBcrypt(t *testing.T) {
	stored, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if !verify(t, "bcrypt", "secret", string(stored)) {
		t.Fatal("correct password rejected")
	}
	if verify(t, "bcrypt", "wrong", string(stored)) {
		t.Fatal("wrong password accepted")
	}
}

func TestPlain(t *testing.T) {
	if !verify(t, "plain", "secret", "secret") {
		t.Fatal("correct password rejected")
	}
	if verify(t, "plain", "secret", "other") {
		t.Fatal("wrong password accepted")
	}
}

func TestUnknownScheme(t *testing.T) {
	if _, err := hash.Get("nope"); err == nil {
		t.Fatal("expected error for unknown scheme")
	}
}

func TestArgon2idAcceptsForeignParameters(t *testing.T) {
	verifier, err := hash.Get("argon2id")
	if err != nil {
		t.Fatal(err)
	}
	for _, params := range []*argon2id.Params{
		{Memory: 19456, Iterations: 2, Parallelism: 1, SaltLength: 16, KeyLength: 32},
		{Memory: 65536, Iterations: 3, Parallelism: 4, SaltLength: 16, KeyLength: 32},
		{Memory: 4096, Iterations: 1, Parallelism: 2, SaltLength: 8, KeyLength: 16},
	} {
		encoded, err := argon2id.CreateHash("правильный-пароль", params)
		if err != nil {
			t.Fatal(err)
		}
		ok, err := verifier.Verify("правильный-пароль", encoded)
		if err != nil || !ok {
			t.Fatalf("m=%d t=%d p=%d: not accepted (%v)", params.Memory, params.Iterations, params.Parallelism, err)
		}
		ok, err = verifier.Verify("не тот пароль", encoded)
		if err != nil || ok {
			t.Fatalf("m=%d t=%d p=%d: wrong password accepted", params.Memory, params.Iterations, params.Parallelism)
		}
	}
}
