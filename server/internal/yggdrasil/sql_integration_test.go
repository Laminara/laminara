package yggdrasil_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/laminara/laminara/server/internal/auth"
)

func TestPostgresYggdrasilIdentity(t *testing.T) {
	dsn := os.Getenv("LAMINARA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set LAMINARA_TEST_POSTGRES_DSN to a disposable PostgreSQL database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	for _, custom := range []bool{false, true} {
		t.Run(fmt.Sprintf("custom_query=%v", custom), func(t *testing.T) {
			table := fmt.Sprintf("ygg_identity_%d", time.Now().UnixNano())
			if _, err := db.Exec(`CREATE TABLE ` + table + ` (name TEXT PRIMARY KEY, password TEXT NOT NULL, uuid UUID)`); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := db.Exec(`DROP TABLE ` + table); err != nil {
					t.Error(err)
				}
			}()
			const uuid = "8667ba71-b85a-4004-af54-457a9734eed7"
			const id = "8667ba71b85a4004af54457a9734eed7"
			if _, err := db.Exec(`INSERT INTO `+table+` VALUES ($1,$2,$3)`, "AuditPlayer", "test", uuid); err != nil {
				t.Fatal(err)
			}
			cfg := map[string]any{"driver": "postgres", "dsn": dsn, "table": table, "hash": "plain", "fields": map[string]string{"username": "name", "password": "password", "uuid": "uuid"}}
			if custom {
				cfg["query"] = `SELECT password, uuid FROM ` + table + ` WHERE name = $1`
			}
			raw, _ := json.Marshal(cfg)
			provider, err := auth.BuildProvider("sql", raw)
			if err != nil {
				t.Fatal(err)
			}
			api := httptest.NewServer(newHandlerWithProvider(t, provider))
			defer api.Close()
			lookup := func(names []string, wantName string) {
				t.Helper()
				resp := post(t, api.URL+"/api/profiles/minecraft", names)
				defer resp.Body.Close()
				var profiles []map[string]string
				if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
					t.Fatal(err)
				}
				if resp.StatusCode != 200 {
					t.Fatalf("lookup status: %d %+v", resp.StatusCode, profiles)
				}
				if wantName == "" {
					if len(profiles) != 0 {
						t.Fatalf("deleted/unknown account returned: %+v", profiles)
					}
					return
				}
				if len(profiles) != 1 || profiles[0]["id"] != id || profiles[0]["name"] != wantName {
					t.Fatalf("lookup UUID/name: %+v", profiles)
				}
			}
			name := "auditplayer"
			if custom {
				name = "AuditPlayer"
			}
			lookup([]string{name, "missing"}, "AuditPlayer")
			login := post(t, api.URL+"/authserver/authenticate", map[string]string{"username": name, "password": "test"})
			var tokens struct {
				AccessToken     string
				SelectedProfile map[string]string
			}
			if err := json.NewDecoder(login.Body).Decode(&tokens); err != nil {
				t.Fatal(err)
			}
			login.Body.Close()
			if login.StatusCode != 200 || tokens.SelectedProfile["id"] != id || tokens.SelectedProfile["name"] != "AuditPlayer" {
				t.Fatalf("login: %d %+v", login.StatusCode, tokens)
			}
			join := post(t, api.URL+"/sessionserver/session/minecraft/join", map[string]string{"accessToken": tokens.AccessToken, "selectedProfile": id, "serverId": "postgres-check"})
			join.Body.Close()
			if join.StatusCode != 204 {
				t.Fatalf("join: %d", join.StatusCode)
			}
			joined, err := http.Get(api.URL + "/sessionserver/session/minecraft/hasJoined?username=AuditPlayer&serverId=postgres-check")
			if err != nil {
				t.Fatal(err)
			}
			var profile map[string]any
			if err := json.NewDecoder(joined.Body).Decode(&profile); err != nil {
				t.Fatal(err)
			}
			joined.Body.Close()
			if joined.StatusCode != 200 || profile["id"] != id {
				t.Fatalf("hasJoined: %d %+v", joined.StatusCode, profile)
			}
			if _, err := db.Exec(`UPDATE `+table+` SET name=$1 WHERE name=$2`, "RenamedPlayer", "AuditPlayer"); err != nil {
				t.Fatal(err)
			}
			lookup([]string{"AuditPlayer", "RenamedPlayer"}, "RenamedPlayer")
			restarted := httptest.NewServer(newHandlerWithProvider(t, provider))
			response := post(t, restarted.URL+"/api/profiles/minecraft", []string{"RenamedPlayer"})
			var profiles []map[string]string
			decodeErr := json.NewDecoder(response.Body).Decode(&profiles)
			response.Body.Close()
			restarted.Close()
			if decodeErr != nil || response.StatusCode != 200 || len(profiles) != 1 || profiles[0]["id"] != id {
				t.Fatalf("lookup after restart: %d %+v %v", response.StatusCode, profiles, decodeErr)
			}
			if _, err := db.Exec(`DELETE FROM ` + table); err != nil {
				t.Fatal(err)
			}
			lookup([]string{"RenamedPlayer"}, "")
		})
	}
}
