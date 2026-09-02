package launchersvc

import (
	"errors"
	"fmt"
	"io"

	"github.com/laminara/laminara/server/internal/clientconfig"
	"github.com/laminara/laminara/server/internal/icon"
	"github.com/laminara/laminara/server/internal/winres"
)

func brand(image []byte, document clientconfig.Document, out io.Writer) []byte {
	if document.Branding == nil || document.Branding.LogoDataURI == "" {
		return image
	}
	logo, err := icon.FromDataURI(document.Branding.LogoDataURI)
	if err != nil {
		if !errors.Is(err, icon.ErrNoPicture) {
			fmt.Fprintf(out, "  иконку взять не вышло, оставляю стандартную: %v\n", err)
		}
		return image
	}
	painted, err := winres.SetIcon(image, logo)
	if err != nil {
		fmt.Fprintf(out, "  иконку заменить не вышло, оставляю стандартную: %v\n", err)
		return image
	}
	fmt.Fprintln(out, "  иконка — из оформления проекта")
	return painted
}
