package hash

import "crypto/subtle"

func init() {
	Register("plain", plainVerifier{})
}

type plainVerifier struct{}

func (plainVerifier) Verify(password, stored string) (bool, error) {
	return subtle.ConstantTimeCompare([]byte(password), []byte(stored)) == 1, nil
}

func (plainVerifier) Hash(password string) (string, error) {
	return password, nil
}
