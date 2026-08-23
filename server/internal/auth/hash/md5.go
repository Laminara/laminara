package hash

import "crypto/md5"

func init() {
	Register("md5", md5Verifier{})
}

type md5Verifier struct{}

func (md5Verifier) Verify(password, stored string) (bool, error) {
	sum := md5.Sum([]byte(password))
	return constantTimeHexEqual(sum[:], stored), nil
}

func (md5Verifier) Hash(password string) (string, error) {
	sum := md5.Sum([]byte(password))
	return hexOf(sum[:]), nil
}
