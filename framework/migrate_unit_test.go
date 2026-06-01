package framework

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// TestMigrateEntity_SingleTable covers the single-entity helpers (no registry,
// dialect auto-detected) on both engines.
func TestMigrateEntity_SingleTable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		ent := entity.Define("solo", entity.EntityConfig{
			Table:  "solo",
			Fields: []schema.Field{{Name: "name", Type: schema.String}},
		}.WithTimestamps(false))

		if err := migrate.MigrateEntity(db, ent); err != nil {
			t.Fatalf("MigrateEntity: %v", err)
		}
		// Idempotent re-run via the explicit-dialect variant.
		if err := migrate.MigrateEntityDialect(db, ent, dialect); err != nil {
			t.Fatalf("MigrateEntityDialect: %v", err)
		}
		cols := liveColumns(t, db, "solo")
		for _, want := range []string{"id", "name"} {
			if _, ok := cols[want]; !ok {
				t.Errorf("MigrateEntity missing column %q; got %v", want, keysOf(cols))
			}
		}
	})
}

// TestTableExistsBulk_SQLite covers the SQLite branch of TableExistsBulk, which
// AutoMigrate itself only invokes for Postgres.
func TestTableExistsBulk_SQLite(t *testing.T) {
	db := openTestDB(t, DialectSQLite)
	if _, err := db.Exec("CREATE TABLE present (id INTEGER)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := migrate.TableExistsBulk(context.Background(), db, []string{"present", "absent"}, DialectSQLite)
	if err != nil {
		t.Fatalf("TableExistsBulk: %v", err)
	}
	if !got["present"] {
		t.Error("expected 'present' to exist")
	}
	if got["absent"] {
		t.Error("expected 'absent' to not exist")
	}
	// Empty input is a clean empty result.
	empty, err := migrate.TableExistsBulk(context.Background(), db, nil, DialectSQLite)
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty TableExistsBulk: %v / %v", empty, err)
	}
}

// TestDestructiveChangeError_Message pins the refusal message.
func TestDestructiveChangeError_Message(t *testing.T) {
	e := &migrate.DestructiveChangeError{Summaries: []string{"posts: drop column a", "posts: drop column b"}}
	msg := e.Error()
	for _, want := range []string{"2 destructive", "drop column a", "drop column b"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q: %s", want, msg)
		}
	}
}

// TestManagedColumns_NotDroppedAcrossConfigs exercises every branch of the
// framework-managed-column guard: a live column that the entity doesn't
// declare must NOT be dropped when it's a timestamps/soft-delete/tenant column
// the config enables, but a genuinely unknown column still drops.
func TestManagedColumns_NotDroppedAcrossConfigs(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		if _, err := db.Exec(`CREATE TABLE acct (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			created_at TIMESTAMP,
			updated_at TIMESTAMP,
			deleted_at TIMESTAMP,
			tenant_id TEXT,
			junk TEXT
		)`); err != nil {
			t.Fatalf("create: %v", err)
		}
		reg := NewRegistry()
		reg.Register(entity.Define("acct", entity.EntityConfig{
			Table:       "acct",
			SoftDelete:  true,
			MultiTenant: true,
			Fields:      []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		})) // timestamps default on

		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		var drops []string
		for _, c := range changes {
			if strings.Contains(c.SQL, "DROP COLUMN") {
				drops = append(drops, c.Summary)
			}
		}
		// Exactly one drop: the genuinely-unmanaged "junk" column.
		if len(drops) != 1 || !strings.Contains(drops[0], "junk") {
			t.Fatalf("expected only 'junk' to drop, got %v", drops)
		}
	})
}
