package storage

import (
	"crypto/rand"
	"os"

	"golang.org/x/sys/windows"
)

func createStagingFile() (*os.File, error) {
	root, err := os.OpenRoot(os.TempDir())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	return root.OpenFile("laminara-cas-"+rand.Text(), os.O_RDWR|os.O_CREATE|os.O_EXCL|windows.O_FILE_FLAG_DELETE_ON_CLOSE, 0o600)
}
