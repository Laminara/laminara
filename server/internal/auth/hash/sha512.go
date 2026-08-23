package hash

import "crypto/sha512"

func init() {
	Register("sha512", sha512Verifier{})
}

type sha512Verifier struct{}

func (sha512Verifier) Verify(password, stored string) (bool, error) {
	sum := sha512.Sum512([]byte(password))
	return constantTimeHexEqual(sum[:], stored), nil
}

func (sha512Verifier) Hash(password string) (string, error) {
	sum := sha512.Sum512([]byte(password))
	return hexOf(sum[:]), nil
}
