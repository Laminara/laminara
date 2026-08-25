//go:build !unix

package cli

import "errors"

func relaunch() error {
	return errors.New("перезапуск на месте умеет только Linux")
}
