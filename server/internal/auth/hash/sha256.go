package hash

import "crypto/sha256"

func init() {
	Register("sha256", sha256Verifier{})
}

type sha256Verifier struct{}

func (sha256Verifier) Verify(password, stored string) (bool, error) {
	sum := sha256.Sum256([]byte(password))
	return constantTimeHexEqual(sum[:], stored), nil
}

func (sha256Verifier) Hash(password string) (string, error) {
	sum := sha256.Sum256([]byte(password))
	return hexOf(sum[:]), nil
}
