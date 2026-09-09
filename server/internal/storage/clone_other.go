//go:build !linux

package storage

import (
	"errors"
	"os"
)

var errCloneUnsupported = errors.New("общие блоки файловой системы умеет только Linux")

func cloneFile(*os.File, string) error {
	return errCloneUnsupported
}

func cloneUnsupported(error) bool {
	return true
}
