package framework

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	coremig "github.com/DonaldMurillo/gofastr/core/migrate"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

func postsReg(fields ...schema.Field) *Registry {
	reg := NewRegistry()
	reg.Register(entity.Define("posts", entity.EntityConfig{Table: "posts", Fields: fields}.WithTimestamps(false)))
	return reg
}

// TestGenerate_FirstMigrationCreatesTable: from an empty snapshot, the first
// generation is a full CREATE TABLE with a DROP TABLE down.
func TestGenerate_FirstMigrationCreatesTable(t *testing.T) {
	reg := postsReg(schema.Field{Name: "title", Type: schema.String, Required: true})
	empty := migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}

	up, down, next, err := migrate.GenerateMigration(reg, empty, DialectSQLite)
	if err != nil {
		t.Fatalf("GenerateMigration: %v", err)
	}
	if !strings.Contains(up, "CREATE TABLE IF NOT EXISTS posts") {
		t.Fatalf("up missing CREATE TABLE: %s", up)
	}
	if !strings.Contains(down, "DROP TABLE IF EXISTS posts") {
		t.Fatalf("down missing DROP TABLE: %s", down)
	}
	if _, ok := next.Tables["posts"]; !ok {
		t.Fatal("next snapshot missing posts table")
	}
}

// TestGenerate_IncrementalAddColumn: a second generation against the prior
// snapshot emits ONLY the new column's ALTER, with a reversible drop.
func TestGenerate_IncrementalAddColumn(t *testing.T) {
	reg1 := postsReg(schema.Field{Name: "title", Type: schema.String, Required: true})
	_, _, snap1, err := migrate.GenerateMigration(reg1, migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}, DialectSQLite)
	if err != nil {
		t.Fatalf("gen1: %v", err)
	}

	reg2 := postsReg(
		schema.Field{Name: "title", Type: schema.String, Required: true},
		schema.Field{Name: "views", Type: schema.Int},
	)
	up, down, _, err := migrate.GenerateMigration(reg2, snap1, DialectSQLite)
	if err != nil {
		t.Fatalf("gen2: %v", err)
	}
	if strings.Contains(up, "CREATE TABLE") {
		t.Fatalf("incremental migration should not recreate the table: %s", up)
	}
	if !strings.Contains(up, "ADD COLUMN views") {
		t.Fatalf("up missing ADD COLUMN views: %s", up)
	}
	if !strings.Contains(down, "DROP COLUMN views") {
		t.Fatalf("down missing DROP COLUMN views: %s", down)
	}
}

// TestGenerate_NoChangesEmptyUp: regenerating with no schema change yields an
// empty up (nothing to write).
func TestGenerate_NoChangesEmptyUp(t *testing.T) {
	reg := postsReg(schema.Field{Name: "title", Type: schema.String, Required: true})
	_, _, snap, _ := migrate.GenerateMigration(reg, migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}, DialectSQLite)
	up, _, _, err := migrate.GenerateMigration(reg, snap, DialectSQLite)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if up != "" {
		t.Fatalf("expected empty up for an unchanged schema, got: %s", up)
	}
}

// TestGenerate_DropTable: removing an entity emits a DROP TABLE with a CREATE
// TABLE down reconstructed from the snapshot.
func TestGenerate_DropTable(t *testing.T) {
	reg1 := postsReg(schema.Field{Name: "title", Type: schema.String, Required: true})
	_, _, snap1, _ := migrate.GenerateMigration(reg1, migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}, DialectSQLite)

	// Now the registry no longer has posts.
	empty := NewRegistry()
	up, down, _, err := migrate.GenerateMigration(empty, snap1, DialectSQLite)
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if !strings.Contains(up, "DROP TABLE IF EXISTS posts") {
		t.Fatalf("up missing DROP TABLE: %s", up)
	}
	if !strings.Contains(down, "CREATE TABLE IF NOT EXISTS posts") {
		t.Fatalf("down missing reconstructed CREATE TABLE: %s", down)
	}
}

// TestGenerate_AppliesAndRollsBackThroughRunner is the end-to-end proof: the
// generated Up/Down round-trips through the versioned runner on a real database
// (both dialects) — apply, then roll back, then re-apply.
func TestGenerate_AppliesAndRollsBackThroughRunner(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, dialect Dialect) {
		// Gen 1: create table.
		reg1 := postsReg(schema.Field{Name: "title", Type: schema.String, Required: true})
		up1, down1, snap1, err := migrate.GenerateMigration(reg1, migrate.SchemaSnapshot{Tables: map[string]map[string]string{}}, dialect)
		if err != nil {
			t.Fatalf("gen1: %v", err)
		}
		// Gen 2: add a column.
		reg2 := postsReg(
			schema.Field{Name: "title", Type: schema.String, Required: true},
			schema.Field{Name: "views", Type: schema.Int},
		)
		up2, down2, _, err := migrate.GenerateMigration(reg2, snap1, dialect)
		if err != nil {
			t.Fatalf("gen2: %v", err)
		}

		m := coremig.New(db, coremig.WithDialect(dialect))
		m.Register(coremig.Migration{Version: 1, Name: "create_posts", Up: up1, Down: down1})
		m.Register(coremig.Migration{Version: 2, Name: "add_views", Up: up2, Down: down2})

		ctx := context.Background()
		if err := m.Up(ctx); err != nil {
			t.Fatalf("Up: %v", err)
		}
		// Both columns insertable.
		if _, err := db.Exec("INSERT INTO posts (id, title, views) VALUES ($1, $2, $3)", "p1", "hi", 3); err != nil {
			t.Fatalf("insert after up: %v", err)
		}

		// Roll back the add-column migration; views must be gone.
		if err := m.Down(ctx, 1); err != nil {
			t.Fatalf("Down(1): %v", err)
		}
		cols := liveColumns(t, db, "posts")
		if _, ok := cols["views"]; ok {
			t.Fatal("down did not remove the views column")
		}
		if _, ok := cols["title"]; !ok {
			t.Fatal("down wrongly removed title")
		}

		// Re-apply.
		if err := m.Up(ctx); err != nil {
			t.Fatalf("re-Up: %v", err)
		}
		cols = liveColumns(t, db, "posts")
		if _, ok := cols["views"]; !ok {
			t.Fatal("re-up did not restore the views column")
		}
	})
}

// TestSnapshot_RoundTripFile covers Load/Save persistence.
func TestSnapshot_RoundTripFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "schema.snapshot.json")

	// Missing file → empty snapshot, no error.
	empty, err := migrate.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("Load missing: %v", err)
	}
	if len(empty.Tables) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", empty)
	}

	reg := postsReg(schema.Field{Name: "title", Type: schema.String})
	snap := migrate.SnapshotFromRegistry(reg, DialectSQLite)
	if err := migrate.SaveSnapshot(path, snap); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := migrate.LoadSnapshot(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := loaded.Tables["posts"]["title"]; !ok {
		t.Fatalf("round-tripped snapshot lost the title column: %+v", loaded)
	}
}
