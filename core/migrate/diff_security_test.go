package migrate

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestDiffHonorsDialect asserts that a Migrator constructed with
// WithDialect(DialectSQLite) emits DDL and runs introspection the SQLite
// engine can actually execute. Diff must not silently fall back to
// Postgres-only catalog (information_schema) or Postgres-only types
// (BIGSERIAL/JSONB/UUID/NOW()).
func TestDiffHonorsDialect(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	m := New(db, WithDialect(DialectSQLite))
	ctx := context.Background()

	ent := Entity{
		Name: "widgets",
		Schema: schema.Schema{
			Fields: []schema.Field{
				{Name: "name", Type: schema.String, Required: true},
				{Name: "count", Type: schema.Int},
				{Name: "meta", Type: schema.JSON},
			},
		},
	}

	// Property 1: Diff must complete against SQLite (no information_schema).
	migs, err := m.Diff(ctx, []Entity{ent})
	if err != nil {
		t.Fatalf("Diff against sqlite: %v", err)
	}
	if len(migs) != 1 {
		t.Fatalf("expected 1 create migration, got %d", len(migs))
	}

	up := migs[0].Up

	// Property 2: emitted DDL must not contain Postgres-only syntax.
	for _, bad := range []string{"BIGSERIAL", "JSONB", "NOW()", "DOUBLE PRECISION"} {
		if strings.Contains(up, bad) {
			t.Errorf("SQLite DDL contains Postgres-only token %q: %s", bad, up)
		}
	}

	// Property 3: the emitted DDL must actually execute on SQLite.
	if _, err := db.ExecContext(ctx, up); err != nil {
		t.Fatalf("SQLite rejected generated DDL: %v\nDDL: %s", err, up)
	}

	// Property 4: a second Diff after the table exists must detect no new
	// table (introspection works) and find the existing columns.
	migs2, err := m.Diff(ctx, []Entity{ent})
	if err != nil {
		t.Fatalf("second Diff against sqlite: %v", err)
	}
	for _, mg := range migs2 {
		if strings.HasPrefix(mg.Name, "auto_create_") {
			t.Errorf("table re-created on second diff; introspection failed: %s", mg.Up)
		}
	}
}

// TestDiffPostgresDDLUnchanged is the happy path: the Postgres dialect must
// still emit its existing Postgres syntax (no regression from the dialect
// branch).
func TestDiffPostgresDDLUnchanged(t *testing.T) {
	m, _ := newTestMigrator(t)

	s := schema.Schema{
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
			{Name: "meta", Type: schema.JSON},
		},
	}

	ddl := m.generateCreateTable("users", s)
	if !strings.Contains(ddl, "id BIGSERIAL PRIMARY KEY") {
		t.Errorf("postgres DDL missing BIGSERIAL id: %s", ddl)
	}
	if !strings.Contains(ddl, "JSONB") {
		t.Errorf("postgres DDL missing JSONB: %s", ddl)
	}
	if !strings.Contains(ddl, "NOW()") {
		t.Errorf("postgres DDL missing NOW(): %s", ddl)
	}
}
