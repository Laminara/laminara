//go:build linux

package storage

import (
	"errors"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

func cloneFile(dst *os.File, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()
	return unix.IoctlFileClone(int(dst.Fd()), int(src.Fd()))
}

func cloneUnsupported(err error) bool {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return false
	}
	switch errno {
	case unix.EOPNOTSUPP, unix.ENOTTY, unix.EXDEV, unix.EINVAL, unix.EPERM, unix.ENOSYS, unix.EISDIR:
		return true
	}
	return false
}
