package yggdrasil_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laminara/laminara/server/internal/auth"
	_ "github.com/laminara/laminara/server/internal/auth/providers"
)

func TestAuthoritativeProfileBeforeLoginAndRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json")
	const id = "8667ba71b85a4004af54457a9734eed7"
	write := func(name string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(`[{"username":"`+name+`","password":"test","uuid":"`+id+`"}]`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("AuditPlayer")
	cfg, _ := json.Marshal(map[string]any{"path": path, "hash": "plain"})
	provider, err := auth.BuildProvider("jsonfile", cfg)
	if err != nil {
		t.Fatal(err)
	}
	api := httptest.NewServer(newHandlerWithProvider(t, provider))
	defer api.Close()
	lookup := func(names []string, expected string) {
		t.Helper()
		resp := post(t, api.URL+"/api/profiles/minecraft", names)
		defer resp.Body.Close()
		var profiles []map[string]string
		if err := json.NewDecoder(resp.Body).Decode(&profiles); err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != 200 || len(profiles) != 1 || profiles[0]["id"] != id || profiles[0]["name"] != expected {
			t.Fatalf("lookup: %d %+v", resp.StatusCode, profiles)
		}
	}
	lookup([]string{"auditplayer", "missing"}, "AuditPlayer")
	login := post(t, api.URL+"/authserver/authenticate", map[string]string{"username": "AuditPlayer", "password": "test"})
	var tokens struct {
		AccessToken     string
		SelectedProfile map[string]string
	}
	if err := json.NewDecoder(login.Body).Decode(&tokens); err != nil {
		t.Fatal(err)
	}
	login.Body.Close()
	if tokens.SelectedProfile["id"] != id {
		t.Fatalf("login: %+v", tokens)
	}
	for _, selected := range []string{"", "00000000000000000000000000000001"} {
		bad := post(t, api.URL+"/sessionserver/session/minecraft/join", map[string]string{"accessToken": tokens.AccessToken, "serverId": "challenge", "selectedProfile": selected})
		bad.Body.Close()
		if bad.StatusCode != 403 {
			t.Fatalf("invalid selected profile %q accepted: %d", selected, bad.StatusCode)
		}
	}
	good := post(t, api.URL+"/sessionserver/session/minecraft/join", map[string]string{"accessToken": tokens.AccessToken, "serverId": "challenge", "selectedProfile": id})
	good.Body.Close()
	if good.StatusCode != 204 {
		t.Fatalf("join: %d", good.StatusCode)
	}
	joined, err := http.Get(api.URL + "/sessionserver/session/minecraft/hasJoined?username=AuditPlayer&serverId=challenge")
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	json.NewDecoder(joined.Body).Decode(&profile)
	joined.Body.Close()
	if joined.StatusCode != 200 || profile["id"] != id {
		t.Fatalf("hasJoined: %d %+v", joined.StatusCode, profile)
	}
	write("RenamedPlayer")
	lookup([]string{"AuditPlayer", "renamedplayer"}, "RenamedPlayer")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	unavailable := post(t, api.URL+"/api/profiles/minecraft", []string{"RenamedPlayer"})
	unavailable.Body.Close()
	if unavailable.StatusCode != 503 {
		t.Fatalf("source failure: %d", unavailable.StatusCode)
	}
}

func TestLookupRejectsMalformedAndOversizedRequests(t *testing.T) {
	api := httptest.NewServer(newHandler(t))
	defer api.Close()
	for _, body := range []string{`["neo"] {}`, `["neo"] trailing`} {
		response, err := http.Post(api.URL+"/api/profiles/minecraft", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != 400 {
			t.Fatalf("malformed request: %d", response.StatusCode)
		}
	}
	response := post(t, api.URL+"/api/profiles/minecraft", make([]string, 101))
	response.Body.Close()
	if response.StatusCode != 400 {
		t.Fatalf("unbounded batch accepted: %d", response.StatusCode)
	}
}

func TestRefreshRejectsAnotherProfileWithoutRevokingToken(t *testing.T) {
	api := httptest.NewServer(newHandler(t))
	defer api.Close()
	account := signIn(t, api.URL)
	response := post(t, api.URL+"/authserver/refresh", map[string]any{"accessToken": account.accessToken, "selectedProfile": map[string]string{"id": "00000000000000000000000000000001", "name": "someone"}})
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatalf("wrong profile accepted: %d", response.StatusCode)
	}
	same := post(t, api.URL+"/authserver/refresh", map[string]any{"accessToken": account.accessToken, "selectedProfile": map[string]string{"id": account.uuid, "name": "neo"}})
	same.Body.Close()
	if same.StatusCode != 400 {
		t.Fatalf("rebinding an assigned profile: %d", same.StatusCode)
	}
	valid := post(t, api.URL+"/authserver/validate", map[string]string{"accessToken": account.accessToken, "clientToken": account.clientToken})
	valid.Body.Close()
	if valid.StatusCode != 204 {
		t.Fatalf("failed refresh revoked original token: %d", valid.StatusCode)
	}
	for _, id := range []string{account.uuid, strings.ToUpper(account.uuid)} {
		if status := profileStatus(t, api.URL, id); status != 200 {
			t.Fatalf("profile UUID %s: %d", id, status)
		}
	}
}

func TestExplicitEmptyClientTokenDoesNotBypassValidation(t *testing.T) {
	api := httptest.NewServer(newHandler(t))
	defer api.Close()
	account := signIn(t, api.URL)
	for _, path := range []string{"validate", "refresh"} {
		response := post(t, api.URL+"/authserver/"+path, map[string]string{"accessToken": account.accessToken, "clientToken": ""})
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("%s accepted explicit empty client token: %d", path, response.StatusCode)
		}
	}
	response := post(t, api.URL+"/authserver/refresh", map[string]any{"accessToken": account.accessToken, "requestUser": true})
	defer response.Body.Close()
	var body struct {
		SelectedProfile map[string]string
		User            struct {
			ID         string
			Properties []any
		}
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 || body.User.ID != body.SelectedProfile["id"] || body.User.Properties == nil {
		t.Fatalf("requestUser response: %d %+v", response.StatusCode, body)
	}
}
