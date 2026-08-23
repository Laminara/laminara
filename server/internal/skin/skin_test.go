package skin_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/laminara/laminara/server/internal/skin"
	"github.com/laminara/laminara/server/internal/store/storetest"
)

func TestTemplateProvider(t *testing.T) {
	config, _ := json.Marshal(map[string]any{
		"skin": "https://cdn.example/skins/%nickname%.png",
		"cape": "https://cdn.example/capes/%uuid%.png",
		"slim": true,
	})
	provider, err := skin.Build("template", config)
	if err != nil {
		t.Fatal(err)
	}
	textures, err := provider.Textures(context.Background(), "neo", "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn.example/skins/neo.png" {
		t.Fatalf("skin = %q", textures.SkinURL)
	}
	if textures.CapeURL != "https://cdn.example/capes/abcd.png" {
		t.Fatalf("cape = %q", textures.CapeURL)
	}
	if !textures.Slim {
		t.Fatal("expected slim model")
	}
}

func TestJSONProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{ "skin": "https://cdn/`+r.URL.Path[len("/skins/"):]+`.png", "model": "slim" }`)
	}))
	defer server.Close()

	config, _ := json.Marshal(map[string]any{"url": server.URL + "/skins/%nickname%"})
	provider, err := skin.Build("json", config)
	if err != nil {
		t.Fatal(err)
	}
	textures, err := provider.Textures(context.Background(), "trinity", "efgh")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn/trinity.png" || !textures.Slim {
		t.Fatalf("textures = %+v", textures)
	}
}

func TestSQLProviderReadsColumns(t *testing.T) {
	db, dsn := storetest.Start(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE users (username varchar(50), uuid varchar(36), skin varchar(255), cape varchar(255))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (username, uuid, skin, cape) VALUES (?, ?, ?, ?)`,
		"Strah", "eec62300", "https://cdn.example/a.png", "https://cdn.example/b.png"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO users (username, uuid, skin, cape) VALUES (?, ?, NULL, NULL)`, "bare", "0000"); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"table":  "users",
		"fields": map[string]string{"username": "username", "skin": "skin", "cape": "cape"},
	})
	provider, err := skin.Build("sql", config)
	if err != nil {
		t.Fatal(err)
	}

	textures, err := provider.Textures(ctx, "Strah", "eec62300")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn.example/a.png" || textures.CapeURL != "https://cdn.example/b.png" {
		t.Fatalf("textures = %+v", textures)
	}

	bare, err := provider.Textures(ctx, "bare", "0000")
	if err != nil {
		t.Fatal(err)
	}
	if bare.SkinURL != "" || bare.CapeURL != "" {
		t.Fatalf("expected no textures, got %+v", bare)
	}

	missing, err := provider.Textures(ctx, "nobody", "ffff")
	if err != nil {
		t.Fatalf("an unknown player must not be an error: %v", err)
	}
	if missing.SkinURL != "" {
		t.Fatalf("textures = %+v", missing)
	}
}

func TestSQLProviderCustomQueryAndModel(t *testing.T) {
	db, dsn := storetest.Start(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE profiles (uuid varchar(36), skin varchar(255), cape varchar(255), model varchar(16))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO profiles (uuid, skin, cape, model) VALUES (?, ?, ?, ?)`,
		"abcd", "https://cdn.example/s.png", "", "slim"); err != nil {
		t.Fatal(err)
	}
	config, _ := json.Marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"lookup": "uuid",
		"query":  `SELECT skin, cape, model FROM profiles WHERE uuid = $1`,
	})
	provider, err := skin.Build("sql", config)
	if err != nil {
		t.Fatal(err)
	}
	textures, err := provider.Textures(ctx, "ignored", "abcd")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn.example/s.png" {
		t.Fatalf("skin = %q", textures.SkinURL)
	}
	if !textures.Slim {
		t.Fatal("model column must decide the arm model")
	}
}
