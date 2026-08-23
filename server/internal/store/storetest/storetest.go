package storetest

import (
	"fmt"
	"net"
	"path/filepath"
	"testing"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/uptrace/bun"

	"github.com/laminara/laminara/server/internal/store"
)

func Start(t testing.TB) (*bun.DB, string) {
	t.Helper()
	port := freePort(t)
	dir := t.TempDir()
	postgres := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Port(port).
		Database("laminara_test").
		RuntimePath(filepath.Join(dir, "runtime")).
		DataPath(filepath.Join(dir, "data")))
	if err := postgres.Start(); err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}
	t.Cleanup(func() { _ = postgres.Stop() })

	dsn := fmt.Sprintf("postgres://postgres:postgres@127.0.0.1:%d/laminara_test?sslmode=disable", port)
	db, err := store.Open(store.Config{Driver: store.DriverPostgres, DSN: dsn})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, dsn
}

func freePort(t testing.TB) uint32 {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free port: %v", err)
	}
	defer listener.Close()
	return uint32(listener.Addr().(*net.TCPAddr).Port)
}
