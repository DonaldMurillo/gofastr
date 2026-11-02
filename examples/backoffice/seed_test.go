package main

import (
	"database/sql"
	"strings"
	"testing"
)

// seed reports a failed insert instead of booting a half-seeded demo:
// each insert's error stops the seed and names what it was writing.
func TestSeedReportsFailedInserts(t *testing.T) {
	open := func(t *testing.T, ddl ...string) *sql.DB {
		t.Helper()
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		for _, s := range ddl {
			if _, err := db.Exec(s); err != nil {
				t.Fatalf("ddl: %v", err)
			}
		}
		return db
	}
	t.Run("product", func(t *testing.T) {
		err := seed(open(t))
		if err == nil || !strings.Contains(err.Error(), "seed product") {
			t.Fatalf("seed with no products table = %v; want a seed product error", err)
		}
	})
	t.Run("supplier", func(t *testing.T) {
		db := open(t, `CREATE TABLE products (id TEXT, name TEXT, price REAL, in_stock INTEGER, category TEXT, description TEXT, photo TEXT, specs TEXT, launched_on TEXT)`)
		err := seed(db)
		if err == nil || !strings.HasPrefix(err.Error(), "seed: ") {
			t.Fatalf("seed with no suppliers table = %v; want a seed error", err)
		}
	})
}
