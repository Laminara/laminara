package providers

import (
	"fmt"

	"github.com/google/uuid"
	"github.com/laminara/laminara/server/internal/auth"
)

func accountUUID(raw, name string) (uuid.UUID, error) {
	if name == "" {
		return uuid.Nil, fmt.Errorf("имя игрока не задано")
	}
	if raw == "" {
		return auth.OfflineUUID(name), nil
	}
	id, err := uuid.Parse(raw)
	if err != nil || id == uuid.Nil {
		return uuid.Nil, fmt.Errorf("UUID аккаунта некорректен или равен нулю")
	}
	return id, nil
}
