package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
)

func TestMigrateIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "m.db")
	db, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db2, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	if err := db2.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db2.SQL.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n < 1 {
		t.Fatalf("expected migrations applied, got %d", n)
	}
}
