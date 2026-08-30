package skin_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

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

func TestSQLProviderModelWithoutCape(t *testing.T) {
	db, dsn := storetest.Start(t)
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE profiles (username varchar(50), skin varchar(255), cape varchar(255), model varchar(16))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO profiles (username, skin, cape, model) VALUES (?, ?, ?, ?)`,
		"Strah", "https://cdn.example/s.png", "https://cdn.example/c.png", "slim"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO profiles (username, skin, cape, model) VALUES (?, ?, NULL, ?)`,
		"Andrey", "https://cdn.example/a.png", "classic"); err != nil {
		t.Fatal(err)
	}

	withoutCape, err := skin.Build("sql", marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"table":  "profiles",
		"fields": map[string]string{"username": "username", "skin": "skin", "model": "model"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	textures, err := withoutCape.Textures(ctx, "Strah", "x")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn.example/s.png" || textures.CapeURL != "" {
		t.Fatalf("a config without fields.cape must still select the skin: %+v", textures)
	}
	if !textures.Slim {
		t.Fatal("the model column must reach the query when fields.cape is not set")
	}
	classic, err := withoutCape.Textures(ctx, "Andrey", "y")
	if err != nil {
		t.Fatal(err)
	}
	if classic.SkinURL != "https://cdn.example/a.png" || classic.Slim {
		t.Fatalf("textures = %+v", classic)
	}

	withCape, err := skin.Build("sql", marshal(map[string]any{
		"driver": "postgres",
		"dsn":    dsn,
		"table":  "profiles",
		"fields": map[string]string{"username": "username", "skin": "skin", "cape": "cape", "model": "model"},
	}))
	if err != nil {
		t.Fatal(err)
	}
	both, err := withCape.Textures(ctx, "Strah", "x")
	if err != nil {
		t.Fatal(err)
	}
	if both.SkinURL != "https://cdn.example/s.png" || both.CapeURL != "https://cdn.example/c.png" || !both.Slim {
		t.Fatalf("textures = %+v", both)
	}
}

func TestSQLProviderSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skins.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.ExecContext(ctx, `CREATE TABLE users (username varchar(50), skin varchar(255), model varchar(16))`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO users (username, skin, model) VALUES (?, ?, ?)`,
		"Strah", "https://cdn.example/s.png", "slim"); err != nil {
		t.Fatal(err)
	}

	provider, err := skin.Build("sql", marshal(map[string]any{
		"driver": "sqlite",
		"dsn":    path,
		"table":  "users",
		"fields": map[string]string{"username": "username", "skin": "skin", "model": "model"},
	}))
	if err != nil {
		t.Fatalf("a driver the settings screen offers must build: %v", err)
	}
	textures, err := provider.Textures(ctx, "Strah", "x")
	if err != nil {
		t.Fatal(err)
	}
	if textures.SkinURL != "https://cdn.example/s.png" || !textures.Slim {
		t.Fatalf("textures = %+v", textures)
	}
}

func marshal(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
