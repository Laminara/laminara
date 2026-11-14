package providers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/auth"
)

func TestSQLProfileLookupMatchesAuthentication(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (name TEXT, password TEXT, uuid TEXT); INSERT INTO users VALUES ('Alice','test','8667ba71-b85a-4004-af54-457a9734eed7')`); err != nil {
		t.Fatal(err)
	}
	for _, custom := range []bool{false, true} {
		cfg := map[string]any{"driver": "sqlite", "dsn": path, "hash": "plain", "table": "users", "fields": map[string]string{"username": "name", "password": "password", "uuid": "uuid"}}
		if custom {
			cfg["query"] = "SELECT password, uuid FROM users WHERE name = ?"
		}
		raw, _ := json.Marshal(cfg)
		provider, err := newSQL(raw)
		if err != nil {
			t.Fatal(err)
		}
		p := provider.(*sqlProvider)
		defer p.db.Close()
		input := "alice"
		if custom {
			input = "Alice"
		}
		profile, err := p.LookupProfile(context.Background(), input)
		if err != nil {
			t.Fatal(err)
		}
		login, err := p.Authenticate(context.Background(), auth.Credentials{Username: input, Password: "test"})
		if err != nil {
			t.Fatal(err)
		}
		if profile != login || profile.Username != "Alice" {
			t.Fatalf("identity mismatch: %+v %+v", profile, login)
		}
		if _, err := p.LookupProfile(context.Background(), "missing"); !errors.Is(err, auth.ErrInvalidCredentials) {
			t.Fatalf("unknown: %v", err)
		}
	}
}

func TestHTTPProfileLookupDoesNotSendCredentials(t *testing.T) {
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/auth" {
			if body["password"] != "test" {
				w.WriteHeader(403)
				return
			}
			w.Write([]byte(`{"uuid":"8667ba71-b85a-4004-af54-457a9734eed7","name":"Alice"}`))
			return
		}
		if len(body) != 1 || body["username"] == "" || r.Header.Get("Authorization") != "Bearer test" {
			t.Errorf("invalid lookup: %+v", body)
		}
		if body["username"] == "missing" {
			w.WriteHeader(404)
			return
		}
		if body["username"] == "failure" {
			w.WriteHeader(500)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"8667ba71b85a4004af54457a9734eed7","name":"Alice"}`))
	}))
	defer source.Close()
	raw, _ := json.Marshal(map[string]any{"url": source.URL + "/auth", "lookupURL": source.URL, "uuidField": "uuid", "nameField": "name", "headers": map[string]string{"Authorization": "Bearer test"}})
	provider, err := newHTTP(raw)
	if err != nil {
		t.Fatal(err)
	}
	login, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "alice", Password: "test"})
	if err != nil {
		t.Fatal(err)
	}
	p := provider.(auth.ProfileLookup)
	profile, err := p.LookupProfile(context.Background(), "alice")
	if err != nil || profile.Username != "Alice" {
		t.Fatalf("lookup: %+v %v", profile, err)
	}
	if profile != login {
		t.Fatalf("HTTP identity mismatch: %+v %+v", profile, login)
	}
	if _, err := p.LookupProfile(context.Background(), "missing"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("unknown: %v", err)
	}
	if _, err := p.LookupProfile(context.Background(), "failure"); err == nil || errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("outage hidden: %v", err)
	}
}

func TestInvalidUUIDNeverFallsBackToAnotherIdentity(t *testing.T) {
	for _, id := range []string{"broken", "00000000-0000-0000-0000-000000000000"} {
		t.Run(id, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "users.json")
			if err := os.WriteFile(path, []byte(`[{"username":"Alice","password":"test","uuid":"`+id+`"}]`), 0600); err != nil {
				t.Fatal(err)
			}
			cfg, _ := json.Marshal(map[string]string{"path": path, "hash": "plain"})
			if _, err := newJSONFile(cfg); err == nil {
				t.Fatal("invalid UUID accepted by jsonfile")
			}
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]string{"uuid": id}) }))
			defer source.Close()
			cfg, _ = json.Marshal(map[string]string{"url": source.URL, "uuidField": "uuid"})
			provider, err := newHTTP(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "Alice", Password: "test"}); err == nil {
				t.Fatal("invalid UUID accepted by HTTP")
			}
		})
	}
}

func TestSQLCustomQueryReturnsCanonicalNameFromSameRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (name TEXT, password TEXT, uuid TEXT); INSERT INTO users VALUES ('Alice','test',NULL)`); err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]string{"driver": "sqlite", "dsn": path, "hash": "plain", "query": "SELECT password,uuid,name FROM users WHERE LOWER(name)=LOWER(?)"})
	provider, err := newSQL(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p := provider.(*sqlProvider)
	defer p.db.Close()
	login, err := p.Authenticate(context.Background(), auth.Credentials{Username: "ALICE", Password: "test"})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := p.LookupProfile(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if profile != login || profile.UUID != auth.OfflineUUID("Alice") {
		t.Fatalf("non-canonical offline identity: %+v %+v", profile, login)
	}
	if _, err := db.Exec(`INSERT INTO users VALUES ('alice','other',NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := p.LookupProfile(context.Background(), "Alice"); err == nil {
		t.Fatal("ambiguous account accepted")
	}
}
