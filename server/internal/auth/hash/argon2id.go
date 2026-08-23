package hash

import "github.com/alexedwards/argon2id"

func init() {
	Register("argon2id", argon2idVerifier{})
}

type argon2idVerifier struct{}

func (argon2idVerifier) Verify(password, stored string) (bool, error) {
	return argon2id.ComparePasswordAndHash(password, stored)
}

func (argon2idVerifier) Hash(password string) (string, error) {
	return argon2id.CreateHash(password, argon2id.DefaultParams)
}
