package framework

import (
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Boot-time additive convergence: AutoMigrate creates missing tables AND adds
// missing columns to existing tables (ALTER TABLE ADD COLUMN — the same DDL
// the declarative diff emits). It never drops, renames, or retypes; those
// stay behind `migrate diff --apply [--allow-destructive]`.

// notesRegistry builds a registry with a single "notes" entity carrying the
// given fields, simulating an entity declaration evolving between boots.
func notesRegistry(cfg entity.EntityConfig) *Registry {
	cfg.Table = "notes"
	reg := NewRegistry()
	reg.Register(entity.Define("notes", cfg))
	return reg
}

// TestAutoMigrate_AddsMissingColumn is the tutorial walkthrough regression:
// boot with {title}, add user_id to the declaration, boot again — the column
// must exist and be writable, with no `migrate diff --apply` step in between.
func TestAutoMigrate_AddsMissingColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		v1 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v1); err != nil {
			t.Fatalf("boot 1: %v", err)
		}
		if _, err := db.Exec("INSERT INTO notes(id, title) VALUES ($1, $2)", "n1", "first"); err != nil {
			t.Fatalf("insert v1: %v", err)
		}

		v2 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "user_id", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v2); err != nil {
			t.Fatalf("boot 2: %v", err)
		}
		if _, ok := liveColumns(t, db, "notes")["user_id"]; !ok {
			t.Fatalf("user_id not added; live columns: %v", keysOf(liveColumns(t, db, "notes")))
		}
		if _, err := db.Exec(
			"INSERT INTO notes(id, title, user_id) VALUES ($1, $2, $3)", "n2", "second", "u1",
		); err != nil {
			t.Fatalf("insert with new column: %v", err)
		}
		// Re-run is idempotent.
		if err := AutoMigrate(db, v2); err != nil {
			t.Fatalf("boot 3 (idempotent re-run): %v", err)
		}
	})
}

// TestAutoMigrate_NeverDropsColumns pins the destructive gate: a field
// removed from the declaration leaves the live column (and its data) alone.
func TestAutoMigrate_NeverDropsColumns(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		wide := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "legacy", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, wide); err != nil {
			t.Fatalf("boot 1: %v", err)
		}
		if _, err := db.Exec("INSERT INTO notes(id, title, legacy) VALUES ($1, $2, $3)", "n1", "t", "keep"); err != nil {
			t.Fatalf("insert: %v", err)
		}

		narrow := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, narrow); err != nil {
			t.Fatalf("boot 2: %v", err)
		}
		var got string
		if err := db.QueryRow("SELECT legacy FROM notes WHERE id = $1", "n1").Scan(&got); err != nil {
			t.Fatalf("legacy column dropped or unreadable: %v", err)
		}
		if got != "keep" {
			t.Fatalf("legacy data lost: %q", got)
		}
	})
}

// TestAutoMigrate_RequiredNewColNullable: a Required field with no default is
// added NULLABLE (a NOT NULL ADD COLUMN fails on populated tables) — same
// deferral as `migrate diff` / `migrate generate`.
func TestAutoMigrate_RequiredNewColNullable(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		v1 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v1); err != nil {
			t.Fatalf("boot 1: %v", err)
		}
		if _, err := db.Exec("INSERT INTO notes(id, title) VALUES ($1, $2)", "n1", "pre"); err != nil {
			t.Fatalf("insert: %v", err)
		}

		v2 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "owner", Type: schema.String, Required: true}, // no default
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v2); err != nil {
			t.Fatalf("boot 2 (required col on populated table): %v", err)
		}
		// Column exists but stays nullable until backfilled.
		if _, err := db.Exec("INSERT INTO notes(id, title) VALUES ($1, $2)", "n2", "post"); err != nil {
			t.Fatalf("insert without required-deferred column: %v", err)
		}
	})
}

// TestAutoMigrate_IndexOnAddedColumn: a new field arriving together with an
// index on it must work in one boot — the column is added before index DDL.
func TestAutoMigrate_IndexOnAddedColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		v1 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v1); err != nil {
			t.Fatalf("boot 1: %v", err)
		}

		v2 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{
				{Name: "title", Type: schema.String},
				{Name: "slug", Type: schema.String},
			},
			Indices: []entity.Index{
				{Name: "uq_notes_slug", Columns: []string{"slug"}, Unique: true},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v2); err != nil {
			t.Fatalf("boot 2 (column + index together): %v", err)
		}
		if _, err := db.Exec("INSERT INTO notes(id, title, slug) VALUES ($1, $2, $3)", "n1", "a", "s"); err != nil {
			t.Fatalf("insert: %v", err)
		}
		if _, err := db.Exec("INSERT INTO notes(id, title, slug) VALUES ($1, $2, $3)", "n2", "b", "s"); err == nil {
			t.Fatal("unique index on added column not enforced")
		}
	})
}

// TestAutoMigrate_AddsManagedColumn: enabling SoftDelete on an existing
// entity adds deleted_at on the next boot, same as any declared field.
func TestAutoMigrate_AddsManagedColumn(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		v1 := notesRegistry(entity.EntityConfig{
			Fields: []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v1); err != nil {
			t.Fatalf("boot 1: %v", err)
		}

		v2 := notesRegistry(entity.EntityConfig{
			SoftDelete: true,
			Fields:     []schema.Field{{Name: "title", Type: schema.String}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, v2); err != nil {
			t.Fatalf("boot 2: %v", err)
		}
		if _, ok := liveColumns(t, db, "notes")["deleted_at"]; !ok {
			t.Fatalf("deleted_at not added; live columns: %v", keysOf(liveColumns(t, db, "notes")))
		}
	})
}
