package providers_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/auth"
	_ "github.com/laminara/laminara/server/internal/auth/providers"
	"github.com/laminara/laminara/server/internal/auth/totp"
	"github.com/laminara/laminara/server/internal/store/storetest"
)

func sha256hex(input string) string {
	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:])
}

func TestJSONFileProvider(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	records := []map[string]any{
		{"username": "neo", "password": sha256hex("matrix"), "uuid": "00000000-0000-0000-0000-000000000001"},
	}
	data, _ := json.Marshal(records)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"path": path, "hash": "sha256"})
	provider, err := auth.BuildProvider("jsonfile", config)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "neo", Password: "matrix"})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if identity.Username != "neo" {
		t.Fatalf("got %+v", identity)
	}
	if _, err := provider.Authenticate(context.Background(), auth.Credentials{Username: "neo", Password: "wrong"}); err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestJSONFileProviderTwoFactor(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	records := []map[string]any{
		{"username": "neo", "password": sha256hex("matrix")},
		{"username": "trinity", "password": sha256hex("theone"), "totp": secret},
	}
	data, _ := json.Marshal(records)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{"path": path, "hash": "sha256"})
	provider, err := auth.BuildProvider("jsonfile", config)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone"}); !errors.Is(err, auth.ErrTwoFactorRequired) {
		t.Fatalf("a user with a secret must ask for the code, got %v", err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone", TwoFactorCode: "000000"}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("a wrong code must be rejected as credentials, got %v", err)
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone", TwoFactorCode: code}); err != nil {
		t.Fatalf("the current code must pass: %v", err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone", TwoFactorCode: code}); err != nil {
		t.Fatalf("the launcher checks the code in two methods, so an immediate repeat must pass: %v", err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "neo", Password: "matrix"}); err != nil {
		t.Fatalf("a user without a secret must sign in as before: %v", err)
	}
}

func TestSQLProviderTwoFactor(t *testing.T) {
	db, dsn := storetest.Start(t)
	ctx := context.Background()
	secret := "JBSWY3DPEHPK3PXP"
	if _, err := db.ExecContext(ctx, `CREATE TABLE players (login varchar(32) primary key, password_hash varchar(128), totp_secret varchar(128))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO players (login, password_hash, totp_secret) VALUES (?, ?, ?)`,
		"trinity", sha256hex("theone"), secret); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO players (login, password_hash, totp_secret) VALUES (?, ?, NULL)`,
		"neo", sha256hex("matrix")); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"hash":   "sha256",
		"fields": map[string]string{"username": "players.login", "password": "players.password_hash", "twoFactorSecret": "players.totp_secret"},
	})
	provider, err := auth.BuildProvider("sql", config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone"}); !errors.Is(err, auth.ErrTwoFactorRequired) {
		t.Fatalf("a user with a secret must ask for the code, got %v", err)
	}
	wrong, err := totp.Code("GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone", TwoFactorCode: wrong}); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("a code from another secret must be rejected: %v", err)
	}
	code, err := totp.Code(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone", TwoFactorCode: code}); err != nil {
		t.Fatalf("the right code must pass: %v", err)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "neo", Password: "matrix"}); err != nil {
		t.Fatalf("a user with an empty column must sign in as before: %v", err)
	}
}

func TestSQLProviderRejectsTwoFactorWithCustomQuery(t *testing.T) {
	config, _ := json.Marshal(map[string]any{
		"driver": "postgres",
		"dsn":    "postgres://postgres:postgres@127.0.0.1:1/none?sslmode=disable",
		"hash":   "sha256",
		"query":  "SELECT hash FROM users WHERE username = $1",
		"fields": map[string]string{"twoFactorSecret": "totp"},
	})
	if _, err := auth.BuildProvider("sql", config); err == nil {
		t.Fatal("two-factor with a custom query must fail at startup, not silently ignore the column")
	}
}

func TestSQLProviderWithFieldMapping(t *testing.T) {
	db, dsn := storetest.Start(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE players (login varchar(32) primary key, secret varchar(128))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO players (login, secret) VALUES (?, ?)`, "trinity", sha256hex("theone")); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"hash":   "sha256",
		"fields": map[string]string{"username": "players.login", "password": "players.secret"},
	})
	provider, err := auth.BuildProvider("sql", config)
	if err != nil {
		t.Fatal(err)
	}
	identity, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "theone"})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if identity.Username != "trinity" {
		t.Fatalf("got %+v", identity)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "trinity", Password: "nope"}); err == nil {
		t.Fatal("wrong password accepted")
	}
}

func TestSQLProviderWithCustomQuery(t *testing.T) {
	db, dsn := storetest.Start(t)
	ctx := context.Background()
	for _, statement := range []string{
		`CREATE TABLE site_users (id int primary key, username varchar(50), uuid varchar(36), is_banned int)`,
		`CREATE TABLE site_passwords (user_id int primary key, hash varchar(255))`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	rows := []struct {
		id       int
		username string
		uuid     string
		banned   int
		password string
	}{
		{1, "neo", "4963a89a-5079-4aba-a51b-3cd2987fb1c2", 0, "matrix"},
		{2, "smith", "eec62300-5d01-403b-a511-5a86fdd7527e", 1, "agent"},
	}
	for _, row := range rows {
		if _, err := db.ExecContext(ctx, `INSERT INTO site_users (id, username, uuid, is_banned) VALUES (?,?,?,?)`, row.id, row.username, row.uuid, row.banned); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO site_passwords (user_id, hash) VALUES (?,?)`, row.id, sha256hex(row.password)); err != nil {
			t.Fatal(err)
		}
	}

	config, _ := json.Marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"hash":   "sha256",
		"query": `SELECT p.hash, u.uuid FROM site_users u
		          JOIN site_passwords p ON p.user_id = u.id
		          WHERE u.username = $1 AND u.is_banned = 0`,
	})
	provider, err := auth.BuildProvider("sql", config)
	if err != nil {
		t.Fatal(err)
	}

	identity, err := provider.Authenticate(ctx, auth.Credentials{Username: "neo", Password: "matrix"})
	if err != nil {
		t.Fatalf("a joined password must authenticate: %v", err)
	}
	if identity.UUID.String() != "4963a89a-5079-4aba-a51b-3cd2987fb1c2" {
		t.Fatalf("the site's own uuid must win over the offline one, got %s", identity.UUID)
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "neo", Password: "wrong"}); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "smith", Password: "agent"}); err == nil {
		t.Fatal("a banned account must not sign in")
	}
	if _, err := provider.Authenticate(ctx, auth.Credentials{Username: "nobody", Password: "x"}); err == nil {
		t.Fatal("an unknown account must not sign in")
	}
}
