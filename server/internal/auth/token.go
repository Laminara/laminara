package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"

	"github.com/google/uuid"
)

const secretBytes = 32

var errMalformedToken = errors.New("malformed token")

func newToken(sessionID uuid.UUID) (token, hash string, err error) {
	secret := make([]byte, secretBytes)
	if _, err := rand.Read(secret); err != nil {
		return "", "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(secret)
	return sessionID.String() + "." + encoded, hashSecret(encoded), nil
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func parseToken(token string) (uuid.UUID, string, error) {
	id, secret, ok := strings.Cut(token, ".")
	if !ok || secret == "" {
		return uuid.Nil, "", errMalformedToken
	}
	sessionID, err := uuid.Parse(id)
	if err != nil {
		return uuid.Nil, "", errMalformedToken
	}
	return sessionID, secret, nil
}

func secretMatchesHash(secret, hash string) bool {
	return subtle.ConstantTimeCompare([]byte(hashSecret(secret)), []byte(hash)) == 1
}
