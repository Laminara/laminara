package hash

import (
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

func init() {
	Register("bcrypt", bcryptVerifier{})
}

type bcryptVerifier struct{}

func (bcryptVerifier) Verify(password, stored string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(stored), []byte(password))
	if err == nil {
		return true, nil
	}
	if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
		return false, nil
	}
	return false, err
}

func (bcryptVerifier) Hash(password string) (string, error) {
	sum, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(sum), err
}

func (bcryptVerifier) HashCost(password string, cost int) (string, error) {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		return "", fmt.Errorf("стоимость bcrypt должна быть от %d до %d", bcrypt.MinCost, bcrypt.MaxCost)
	}
	sum, err := bcrypt.GenerateFromPassword([]byte(password), cost)
	return string(sum), err
}

const BcryptDefaultCost = bcrypt.DefaultCost
