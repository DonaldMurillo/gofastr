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

// TestRawTable_ExactColumns: a raw Table migrates with EXACTLY its declared
// columns — no auto-injected id/created_at/updated_at.
func TestRawTable_ExactColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		tbl := migrate.Table{
			Name: "user_roles",
			Columns: []migrate.Column{
				{Name: "user_id", Type: schema.String, NotNull: true},
				{Name: "role", Type: schema.String, NotNull: true},
			},
			Indices: []entity.Index{{Name: "ux_user_roles", Columns: []string{"user_id", "role"}, Unique: true}},
		}
		if err := reg.Register(tbl.ToEntity()); err != nil {
			t.Fatalf("register: %v", err)
		}
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		cols := liveColumns(t, db, "user_roles")
		if len(cols) != 2 {
			t.Fatalf("expected exactly 2 columns (no auto-injection), got %v", keysOf(cols))
		}
		for _, want := range []string{"user_id", "role"} {
			if _, ok := cols[want]; !ok {
				t.Errorf("missing column %q", want)
			}
		}
		for _, unwanted := range []string{"id", "created_at", "updated_at"} {
			if _, ok := cols[unwanted]; ok {
				t.Errorf("raw table wrongly got auto-injected column %q", unwanted)
			}
		}
		// Round-trips clean.
		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("raw table not coherent: %+v", changes)
		}
	})
}

// TestRawTable_RawType: an explicit RawType column survives migration and diffs
// clean.
func TestRawTable_RawType(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		tbl := migrate.Table{
			Name: "ledger",
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true, NotNull: true},
				{Name: "amount", RawType: "NUMERIC(12,2)", NotNull: true},
			},
		}
		_ = reg.Register(tbl.ToEntity())
		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate: %v", err)
		}
		// The NUMERIC column must accept a precise decimal.
		if _, err := db.Exec("INSERT INTO ledger (id, amount) VALUES ($1, $2)", "l1", "10.25"); err != nil {
			t.Fatalf("insert into NUMERIC column: %v", err)
		}
		changes, err := DiffSchema(context.Background(), db, reg)
		if err != nil {
			t.Fatalf("DiffSchema: %v", err)
		}
		if len(changes) != 0 {
			t.Fatalf("RawType table not coherent: %+v", changes)
		}
	})
}

// TestReconcile_EntityAndRawTableWithFK is the headline: an entity and a raw
// Table in ONE registry migrate together, with a foreign key crossing from the
// raw table to the entity, topo-sorted correctly, and enforced at runtime.
func TestReconcile_EntityAndRawTableWithFK(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		reg := NewRegistry()
		// A normal entity (gets id + timestamps).
		reg.Register(entity.Define("users", entity.EntityConfig{
			Table:  "users",
			Fields: []schema.Field{{Name: "name", Type: schema.String, Required: true}},
		}))
		// A raw audit table with an FK to users.id — registered AFTER users, but
		// even reversed the topo sort would order users first.
		audit := migrate.Table{
			Name: "audit_log",
			Columns: []migrate.Column{
				{Name: "id", Type: schema.String, PrimaryKey: true, NotNull: true},
				{Name: "user_id", Type: schema.String, NotNull: true},
				{Name: "action", Type: schema.Text},
			},
			ForeignKeys: []migrate.ForeignKey{{Column: "user_id", RefTable: "users"}},
		}
		_ = reg.Register(audit.ToEntity())

		if err := AutoMigrate(db, reg); err != nil {
			t.Fatalf("AutoMigrate (entity + raw table): %v", err)
		}

		// Insert a user, then an audit row referencing it — must succeed.
		if _, err := db.Exec("INSERT INTO users (id, name) VALUES ($1, $2)", "u1", "alice"); err != nil {
			t.Fatalf("insert user: %v", err)
		}
		if _, err := db.Exec("INSERT INTO audit_log (id, user_id, action) VALUES ($1, $2, $3)", "a1", "u1", "login"); err != nil {
			t.Fatalf("insert audit referencing user: %v", err)
		}
		// An orphan audit row must be rejected by the FK.
		_, err := db.Exec("INSERT INTO audit_log (id, user_id, action) VALUES ($1, $2, $3)", "a2", "ghost", "x")
		if err == nil {
			t.Fatal("expected FK violation for orphan audit row")
		}

		// Both tables diff clean together, and a generated migration would
		// include both.
		changes, derr := DiffSchema(context.Background(), db, reg)
		if derr != nil {
			t.Fatalf("DiffSchema: %v", derr)
		}
		if len(changes) != 0 {
			t.Fatalf("entity+raw reconciliation not coherent: %+v", changes)
		}
		up, _, _, gerr := migrate.GenerateMigration(reg, migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}, migrate.DetectDialect(db))
		if gerr != nil {
			t.Fatalf("GenerateMigration: %v", gerr)
		}
		if !strings.Contains(up, "users") || !strings.Contains(up, "audit_log") {
			t.Fatalf("generated migration should cover both tables:\n%s", up)
		}
	})
}
