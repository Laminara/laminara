package providers

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/auth"
)

func TestJSONFileSnapshotDetectsSameStampEdits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	original := []byte(`[{"username":"Alice","password":"first"}]`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path, "hash": "plain"})
	provider, err := newJSONFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	for _, password := range []string{"other", "first"} {
		if err := os.WriteFile(path, []byte(`[{"username":"Alice","password":"`+password+`"}]`), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		if _, err := provider.Authenticate(t.Context(), auth.Credentials{Username: "alice", Password: password}); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(`broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(t.Context(), auth.Credentials{Username: "Alice", Password: "first"}); err == nil {
		t.Fatal("corrupt source used cached credentials")
	}
}

func TestJSONFileUnicodeNamesAndAccountChanges(t *testing.T) {
	for _, names := range [][2]string{{"Σ", "ς"}, {"K", "K"}, {"s", "ſ"}} {
		t.Run(names[0], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "users.json")
			data, _ := json.Marshal([]map[string]string{{"username": names[0], "password": "old"}})
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			raw, _ := json.Marshal(map[string]string{"path": path, "hash": "plain"})
			provider, err := newJSONFile(raw)
			if err != nil {
				t.Fatal(err)
			}
			resolver := provider.(auth.ProfileLookup)
			identity, err := resolver.LookupProfile(t.Context(), names[1])
			if err != nil || identity.Username != names[0] {
				t.Fatalf("unicode alias: %+v %v", identity, err)
			}
			accounts := provider.(auth.Accounts)
			if _, err := accounts.Add(t.Context(), names[1], "new", ""); err == nil {
				t.Fatal("duplicate alias accepted")
			}
			if err := accounts.SetPassword(t.Context(), names[1], "new"); err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Authenticate(t.Context(), auth.Credentials{Username: names[1], Password: "new"}); err != nil {
				t.Fatal(err)
			}
			if err := accounts.Remove(t.Context(), names[1]); err != nil {
				t.Fatal(err)
			}
			if _, err := resolver.LookupProfile(t.Context(), names[0]); err == nil {
				t.Fatal("removed user remains cached")
			}
			data = []byte(strings.ReplaceAll(string(data), `}]`, `},{"username":"`+names[1]+`","password":"old"}]`))
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := newJSONFile(raw); err == nil {
				t.Fatal("source with duplicate aliases accepted")
			}
		})
	}
}

func jsonFileAccounts(t testing.TB, count int) auth.Provider {
	t.Helper()
	records := make([]map[string]string, count)
	for i := range records {
		records[i] = map[string]string{"username": fmt.Sprintf("Player%08d", i), "password": "test"}
	}
	path := filepath.Join(t.TempDir(), "users.json")
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"path": path, "hash": "plain"})
	provider, err := newJSONFile(raw)
	if err != nil {
		t.Fatal(err)
	}
	return provider
}

func TestJSONFileRepeatedLookupsReuseSnapshot(t *testing.T) {
	provider := jsonFileAccounts(t, 1000).(auth.ProfileLookup)
	allocations := testing.AllocsPerRun(5, func() {
		if _, err := provider.LookupProfile(t.Context(), "player00000000"); err != nil {
			t.Fatal(err)
		}
	})
	if allocations > 64 {
		t.Fatalf("repeated lookup rebuilt the account snapshot: %.0f allocations", allocations)
	}
}

func BenchmarkJSONFileProfileLookup(b *testing.B) {
	for _, count := range []int{1000, 5000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			provider := jsonFileAccounts(b, count)
			resolver := provider.(auth.ProfileLookup)
			b.ResetTimer()
			for b.Loop() {
				for i := 0; i < 100; i++ {
					if _, err := resolver.LookupProfile(b.Context(), fmt.Sprintf("Player%08d", i)); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}
