//go:build !windows

package storage

import "os"

func createStagingFile() (*os.File, error) {
	file, err := os.CreateTemp("", "laminara-cas-*")
	if err != nil {
		return nil, err
	}
	if err := os.Remove(file.Name()); err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	return file, nil
}
