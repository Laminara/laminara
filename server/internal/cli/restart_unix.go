//go:build unix

package cli

import (
	"os"
	"syscall"
)

func relaunch() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	return syscall.Exec(executable, os.Args, os.Environ())
}
