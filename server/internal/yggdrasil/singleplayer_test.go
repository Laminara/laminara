package yggdrasil_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/skin"
	"github.com/laminara/laminara/server/internal/yggdrasil"
)

func newHandlerOn(t *testing.T, sessions *redis.Client) http.Handler {
	t.Helper()
	authService := auth.NewService(stubProvider{}, auth.NewMemorySessionStore(), auth.DefaultConfig())
	skinConfig, _ := json.Marshal(map[string]any{"skin": "https://skins.example/%nickname%.png"})
	skinProvider, err := skin.Build("template", skinConfig)
	if err != nil {
		t.Fatal(err)
	}
	server, err := yggdrasil.NewServer(authService, skinProvider, nil, nil, yggdrasil.Config{
		ServerName:  "Laminara",
		SkinDomains: []string{"skins.example"},
		Sessions:    sessions,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

type signedIn struct {
	accessToken string
	clientToken string
	uuid        string
}

func signIn(t *testing.T, base string) signedIn {
	t.Helper()
	response := post(t, base+"/authserver/authenticate", map[string]string{"username": "neo", "password": "matrix"})
	defer response.Body.Close()
	var body struct {
		AccessToken     string `json:"accessToken"`
		ClientToken     string `json:"clientToken"`
		SelectedProfile struct {
			ID string `json:"id"`
		} `json:"selectedProfile"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return signedIn{accessToken: body.AccessToken, clientToken: body.ClientToken, uuid: body.SelectedProfile.ID}
}

func profileStatus(t *testing.T, base, uuid string) int {
	t.Helper()
	response, err := http.Get(base + "/sessionserver/session/minecraft/profile/" + uuid + "?unsigned=true")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	return response.StatusCode
}

func sharedSessions(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	return server, client
}

func TestSingleplayerSkinSurvivesARestartOfTheServer(t *testing.T) {
	_, client := sharedSessions(t)
	before := httptest.NewServer(newHandlerOn(t, client))
	defer before.Close()
	account := signIn(t, before.URL)

	after := httptest.NewServer(newHandlerOn(t, client))
	defer after.Close()
	if status := profileStatus(t, after.URL, account.uuid); status != http.StatusOK {
		t.Fatalf("в одиночной игре скин берётся из профиля — после перезапуска сервер о нём забыл: %d", status)
	}
}

func TestRefreshBringsTheProfileBack(t *testing.T) {
	sessions, client := sharedSessions(t)
	before := httptest.NewServer(newHandlerOn(t, client))
	defer before.Close()
	account := signIn(t, before.URL)

	sessions.Del("laminara:ygg:profile:" + account.uuid)
	after := httptest.NewServer(newHandlerOn(t, client))
	defer after.Close()
	if status := profileStatus(t, after.URL, account.uuid); status != http.StatusNoContent {
		t.Fatalf("профиль забыли — сервер обязан честно ответить 204, а не %d", status)
	}

	response := post(t, after.URL+"/authserver/refresh", map[string]string{
		"accessToken": account.accessToken,
		"clientToken": account.clientToken,
	})
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("лаунчер продлевает игровую сессию при каждом запуске: %d", response.StatusCode)
	}
	if status := profileStatus(t, after.URL, account.uuid); status != http.StatusOK {
		t.Fatalf("после продления сессии профиль обязан снова отдаваться: %d", status)
	}
}
