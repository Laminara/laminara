package macapp

import (
	"errors"
	"strings"

	"github.com/laminara/laminara/server/internal/icon"
)

func Icon(dataURI string) ([]byte, error) {
	if strings.TrimSpace(dataURI) == "" {
		return nil, nil
	}
	logo, err := icon.FromDataURI(dataURI)
	if err != nil {
		if errors.Is(err, icon.ErrNoPicture) {
			return nil, nil
		}
		return nil, err
	}
	return icon.ICNS(logo)
}
