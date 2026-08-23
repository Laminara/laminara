package store_test

import (
	"context"
	"testing"

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
