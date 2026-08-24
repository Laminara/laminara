//go:build !linux

package storage

import (
	"errors"
	"os"
)

var errCloneUnsupported = errors.New("block sharing is only implemented on linux")

func cloneFile(*os.File, string) error {
	return errCloneUnsupported
}

func cloneUnsupported(error) bool {
	return true
}
