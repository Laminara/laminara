package yggdrasil_test

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/laminara/laminara/server/internal/auth"
	"github.com/laminara/laminara/server/internal/skin"
	"github.com/laminara/laminara/server/internal/yggdrasil"
)

type stubProvider struct{}

func (stubProvider) Authenticate(_ context.Context, creds auth.Credentials) (auth.Identity, error) {
	if creds.Username == "neo" && creds.Password == "matrix" {
		return auth.Identity{Subject: "neo", Username: "neo", UUID: auth.OfflineUUID("neo")}, nil
	}
	return auth.Identity{}, auth.ErrInvalidCredentials
}

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	authService := auth.NewService(stubProvider{}, auth.NewMemorySessionStore(), auth.DefaultConfig())
	skinConfig, _ := json.Marshal(map[string]any{"skin": "https://skins.example/%nickname%.png", "slim": true})
	skinProvider, err := skin.Build("template", skinConfig)
	if err != nil {
		t.Fatal(err)
	}
	server, err := yggdrasil.NewServer(authService, skinProvider, nil, nil, yggdrasil.Config{ServerName: "Laminara", SkinDomains: []string{"skins.example"}})
	if err != nil {
		t.Fatal(err)
	}
	return server.Handler()
}

func post(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestYggdrasilFlow(t *testing.T) {
	server := httptest.NewServer(newHandler(t))
	defer server.Close()

	meta, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	var metaBody struct {
		SignaturePublickey string   `json:"signaturePublickey"`
		SkinDomains        []string `json:"skinDomains"`
	}
	json.NewDecoder(meta.Body).Decode(&metaBody)
	meta.Body.Close()
	if metaBody.SignaturePublickey == "" || len(metaBody.SkinDomains) != 1 {
		t.Fatalf("metadata = %+v", metaBody)
	}
	publicKey := parseRSAPublic(t, metaBody.SignaturePublickey)

	authResp := post(t, server.URL+"/authserver/authenticate", map[string]string{"username": "neo", "password": "matrix"})
	var authBody struct {
		AccessToken     string            `json:"accessToken"`
		SelectedProfile map[string]string `json:"selectedProfile"`
	}
	json.NewDecoder(authResp.Body).Decode(&authBody)
	authResp.Body.Close()
	if authResp.StatusCode != http.StatusOK || authBody.AccessToken == "" || authBody.SelectedProfile["name"] != "neo" {
		t.Fatalf("authenticate failed: %d %+v", authResp.StatusCode, authBody)
	}

	const serverID = "abc123"
	joinResp := post(t, server.URL+"/sessionserver/session/minecraft/join", map[string]string{"accessToken": authBody.AccessToken, "serverId": serverID})
	joinResp.Body.Close()
	if joinResp.StatusCode != http.StatusNoContent {
		t.Fatalf("join = %d", joinResp.StatusCode)
	}

	hasJoined, err := http.Get(server.URL + "/sessionserver/session/minecraft/hasJoined?username=neo&serverId=" + serverID)
	if err != nil {
		t.Fatal(err)
	}
	if hasJoined.StatusCode != http.StatusOK {
		t.Fatalf("hasJoined = %d", hasJoined.StatusCode)
	}
	var profile struct {
		Name       string `json:"name"`
		Properties []struct {
			Name      string `json:"name"`
			Value     string `json:"value"`
			Signature string `json:"signature"`
		} `json:"properties"`
	}
	json.NewDecoder(hasJoined.Body).Decode(&profile)
	hasJoined.Body.Close()
	if profile.Name != "neo" || len(profile.Properties) != 1 || profile.Properties[0].Name != "textures" {
		t.Fatalf("profile = %+v", profile)
	}

	value := profile.Properties[0].Value
	signature, _ := base64.StdEncoding.DecodeString(profile.Properties[0].Signature)
	digest := sha1.Sum([]byte(value))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA1, digest[:], signature); err != nil {
		t.Fatalf("texture signature does not verify: %v", err)
	}
	raw, _ := base64.StdEncoding.DecodeString(value)
	if !bytes.Contains(raw, []byte("https://skins.example/neo.png")) {
		t.Fatalf("skin url not in signed payload: %s", raw)
	}

	bad := post(t, server.URL+"/authserver/authenticate", map[string]string{"username": "neo", "password": "nope"})
	bad.Body.Close()
	if bad.StatusCode != http.StatusForbidden {
		t.Fatalf("bad credentials status = %d", bad.StatusCode)
	}
}

func parseRSAPublic(t *testing.T, pemString string) *rsa.PublicKey {
	t.Helper()
	block, _ := pem.Decode([]byte(pemString))
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return key.(*rsa.PublicKey)
}
