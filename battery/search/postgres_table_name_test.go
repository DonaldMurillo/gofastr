package search

import (
	"context"
	"strings"
	"testing"
)

// An older build kept a configured "Docs" table case-kept. This build
// names docs, so EnsureSchema refuses and prints the rename.
func TestPostgresCaseKeptTableRefused(t *testing.T) {
	db := openSearchPG(t)
	if _, err := db.Exec(`CREATE TABLE "Docs" (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("create the older table: %v", err)
	}
	idx, err := NewPostgres(db, PostgresConfig{Table: "Docs"})
	if err != nil {
		t.Fatalf("NewPostgres: %v", err)
	}
	err = idx.EnsureSchema(context.Background())
	if err == nil || !strings.Contains(err.Error(), `ALTER TABLE "Docs" RENAME TO "docs"`) {
		t.Fatalf("EnsureSchema over \"Docs\": err = %v, want the rename advice", err)
	}
}

// The Postgres index runs under any configured table name: a mixed-case
// Docs names the folded docs that unquoted DDL would create, a
// schema-qualified name puts the table, its indexes and its trigger in
// that schema, and a table named excluded does not collide with the
// EXCLUDED pseudo-row of the upsert.
func TestPostgresConfiguredTableNames(t *testing.T) {
	for _, table := range []string{"Docs", "qualified", "excluded"} {
		t.Run(table, func(t *testing.T) {
			db := openSearchPG(t)
			ctx := context.Background()
			var schema string
			if err := db.QueryRow(`SELECT current_schema()`).Scan(&schema); err != nil {
				t.Fatalf("current schema: %v", err)
			}
			configured, stored := table, table
			switch table {
			case "Docs":
				stored = "docs"
			case "qualified":
				configured, stored = schema+".Docs", "docs"
			}
			idx, err := NewPostgres(db, PostgresConfig{Table: configured})
			if err != nil {
				t.Fatalf("NewPostgres: %v", err)
			}
			if err := idx.EnsureSchema(ctx); err != nil {
				t.Fatalf("EnsureSchema: %v", err)
			}
			var trgOID int64
			trigger := func() int64 {
				t.Helper()
				var oid int64
				if err := db.QueryRow(`SELECT t.oid FROM pg_trigger t
					JOIN pg_class c ON c.oid = t.tgrelid
					JOIN pg_namespace n ON n.oid = c.relnamespace
					WHERE n.nspname = $1 AND c.relname = $2 AND t.tgname = $3`,
					schema, stored, stored+"_tsv_prefix").Scan(&oid); err != nil {
					t.Fatalf("trigger %s_tsv_prefix on %s.%s: %v", stored, schema, stored, err)
				}
				return oid
			}
			trgOID = trigger()
			if err := idx.EnsureSchema(ctx); err != nil {
				t.Fatalf("second EnsureSchema: %v", err)
			}
			if got := trigger(); got != trgOID {
				t.Errorf("second EnsureSchema rebuilt the trigger (oid %d → %d): the probe missed the stored name", trgOID, got)
			}
			var indexes int
			if err := db.QueryRow(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = $1 AND tablename = $2 AND indexname LIKE '%tsv%'`,
				schema, stored).Scan(&indexes); err != nil {
				t.Fatalf("count indexes: %v", err)
			}
			if indexes != 2 {
				t.Errorf("tsv indexes on %s.%s = %d, want 2", schema, stored, indexes)
			}
			doc := Document{ID: "a", Type: "t", Text: "gopher burrow"}
			if err := idx.Index(ctx, doc); err != nil {
				t.Fatalf("index: %v", err)
			}
			doc.Text = "gopher tunnel"
			if err := idx.Index(ctx, doc); err != nil {
				t.Fatalf("reindex over an existing id: %v", err)
			}
			res, err := idx.Search(ctx, Query{Text: "tunn"})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(res) != 1 || res[0].Document.Text != "gopher tunnel" {
				t.Errorf("search tunn = %+v, want the reindexed document", res)
			}
		})
	}
}
