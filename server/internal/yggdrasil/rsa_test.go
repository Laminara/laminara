package yggdrasil

import (
	"crypto/rsa"
	"path/filepath"
	"testing"
)

func TestConcurrentKeyCreationKeepsOneCompleteKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "key.pem")
	type result struct {
		key *rsa.PrivateKey
		err error
	}
	results := make(chan result, 8)
	for range 8 {
		go func() { key, err := loadOrCreateRSA(path); results <- result{key, err} }()
	}
	var first *rsa.PrivateKey
	for range 8 {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if first == nil {
			first = result.key
		} else if first.N.Cmp(result.key.N) != 0 {
			t.Fatal("concurrent servers got different signing keys")
		}
	}
	saved, err := loadOrCreateRSA(path)
	if err != nil {
		t.Fatal(err)
	}
	if first.N.Cmp(saved.N) != 0 {
		t.Fatal("persisted key differs from published key")
	}
}
