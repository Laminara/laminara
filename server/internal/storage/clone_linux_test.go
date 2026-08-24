//go:build linux

package storage

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestCloneFileSharesBlocks(t *testing.T) {
	dir := os.Getenv("LAMINARA_E2E_REFLINK_DIR")
	if dir == "" {
		t.Skip("set LAMINARA_E2E_REFLINK_DIR to a directory on btrfs, XFS or bcachefs")
	}

	payload := bytes.Repeat([]byte("laminara"), 8192)
	source := filepath.Join(dir, "clone-source")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(source)

	target, err := os.CreateTemp(dir, "clone-target-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(target.Name())
	defer target.Close()

	if err := cloneFile(target, source); err != nil {
		t.Fatalf("%s does not support block sharing: %v", dir, err)
	}
	got, err := os.ReadFile(target.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("the clone differs from its source")
	}
}
