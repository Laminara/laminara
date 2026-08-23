package auth

import (
	"crypto/md5"

	"github.com/google/uuid"
)

type Identity struct {
	Subject  string
	Username string
	UUID     uuid.UUID
}

type Credentials struct {
	Username string
	Password string
}

func OfflineUUID(username string) uuid.UUID {
	sum := md5.Sum([]byte("OfflinePlayer:" + username))
	var id uuid.UUID
	copy(id[:], sum[:])
	id[6] = (id[6] & 0x0f) | 0x30
	id[8] = (id[8] & 0x3f) | 0x80
	return id
}
