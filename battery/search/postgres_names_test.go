package search

import (
	"context"
	"strings"
	"testing"
)

// TestPostgresLongTableIndexes: both GIN indexes exist for a table name of
// any length. Postgres keeps 63 bytes of a name, so at 58 bytes the two
// index names cut to the same name and at 63 the first equals the table.
func TestPostgresLongTableIndexes(t *testing.T) {
	db := openSearchPG(t)
	for _, n := range []int{16, 55, 58, 63} {
		table := "t" + strings.Repeat("x", n-1)
		idx, err := NewPostgres(db, PostgresConfig{Table: table})
		if err != nil {
			t.Fatal(err)
		}
		if err := idx.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("len %d: %v", n, err)
		}
		var gin int
		if err := db.QueryRow(`SELECT count(*) FROM pg_indexes
			WHERE tablename = $1 AND schemaname = current_schema() AND indexdef LIKE '%USING gin%'`, table).Scan(&gin); err != nil {
			t.Fatal(err)
		}
		if gin != 2 {
			t.Errorf("table name of %d bytes: %d GIN indexes, want 2", n, gin)
		}
	}
}

// A table a released build set up keeps its one tsv index: released
// builds let Postgres cut the index name at 63 bytes, and the current
// name must be that same cut, or IF NOT EXISTS builds a second index.
func TestPostgresKeepsReleasedTsvIndex(t *testing.T) {
	db := openSearchPG(t)
	table := "t" + strings.Repeat("y", 57)
	idx, err := NewPostgres(db, PostgresConfig{Table: table})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(`SELECT indexname FROM pg_indexes WHERE tablename = $1 AND schemaname = current_schema() AND indexdef LIKE '%USING gin (tsv)'`, table)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, n := range names {
		if _, err := db.Exec(`DROP INDEX "` + n + `"`); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`CREATE INDEX "` + table + `_tsv_idx" ON "` + table + `" USING GIN (tsv)`); err != nil {
		t.Fatal(err)
	}
	if err := idx.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM pg_indexes WHERE tablename = $1 AND schemaname = current_schema() AND indexdef LIKE '%USING gin (tsv)'`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("%d GIN indexes on tsv, want 1", n)
	}
}

// Names that fit keep their spelling, so existing indexes are found
// again; longer ones stay within 63 bytes and apart.
func TestPgRelNameFitsAndDiffers(t *testing.T) {
	if got := pgRelName("search_documents", "_tsv_idx"); got != "search_documents_tsv_idx" {
		t.Errorf("short name changed: %q", got)
	}
	long := strings.Repeat("x", 63)
	a, b := pgRelName(long, "_tsv_idx"), pgRelName(long, "_tsv_prefix_idx")
	if len(a) > 63 || len(b) > 63 || a == b || a == long || b == long {
		t.Errorf("long names %q, %q: want distinct, <= 63 bytes, not the table", a, b)
	}
}
