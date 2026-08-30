package store_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/laminara/laminara/server/internal/sqlschema"
	"github.com/laminara/laminara/server/internal/store"
	"github.com/laminara/laminara/server/internal/store/storetest"
)

func TestConnectivity(t *testing.T) {
	db, _ := storetest.Start(t)
	var result int
	if err := db.QueryRowContext(context.Background(), "SELECT 1").Scan(&result); err != nil {
		t.Fatalf("query: %v", err)
	}
	if result != 1 {
		t.Fatalf("got %d", result)
	}
}

func TestEveryListedDriverOpens(t *testing.T) {
	dsns := map[string]string{
		sqlschema.Postgres: "postgres://user:pass@127.0.0.1:5432/laminara?sslmode=disable",
		sqlschema.MySQL:    "user:pass@tcp(127.0.0.1:3306)/laminara",
		sqlschema.SQLite:   filepath.Join(t.TempDir(), "laminara.db"),
	}
	for _, driver := range sqlschema.Drivers() {
		if _, ok := dsns[driver]; !ok {
			t.Fatalf("driver %q has no dsn to try", driver)
		}
		db, err := store.Open(store.Config{Driver: store.Driver(driver), DSN: dsns[driver]})
		if err != nil {
			t.Fatalf("%s: %v", driver, err)
		}
		if db == nil {
			t.Fatalf("%s opened nothing", driver)
		}
		if err := db.Close(); err != nil {
			t.Fatalf("%s: %v", driver, err)
		}
	}
}

func TestUnknownDriverIsRefused(t *testing.T) {
	if _, err := store.Open(store.Config{Driver: "oracle", DSN: "anything"}); err == nil {
		t.Fatal("an unknown driver must be refused")
	}
}
