package migrate

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestRetypeRebuildKeepsDependentRows: the SQLite retype rebuild drops and
// recreates the parent table. With enforcement on, that DROP runs every ON
// DELETE action against the rows that reference it, so a CASCADE child (a
// generated pivot) lost its rows and a SET NULL child lost its link.
func TestRetypeRebuildKeepsDependentRows(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	var fks int
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fks); err != nil || fks != 1 {
		t.Fatalf("foreign_keys = %d (%v): the test needs enforcement on to mean anything", fks, err)
	}
	for _, s := range []string{
		`CREATE TABLE widgets (id TEXT PRIMARY KEY, count INTEGER NOT NULL DEFAULT 0)`,
		`CREATE TABLE widget_tags (widget_id TEXT NOT NULL REFERENCES widgets(id) ON DELETE CASCADE, tag TEXT NOT NULL)`,
		`CREATE TABLE widget_notes (id TEXT PRIMARY KEY, widget_id TEXT REFERENCES widgets(id) ON DELETE SET NULL)`,
		`INSERT INTO widgets (id, count) VALUES ('w1', 7)`,
		`INSERT INTO widget_tags (widget_id, tag) VALUES ('w1', 'red')`,
		`INSERT INTO widget_notes (id, widget_id) VALUES ('n1', 'w1')`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}

	changes, err := DiffSchema(ctx, db, widgetsTypeReg(t))
	if err != nil {
		t.Fatalf("DiffSchema: %v", err)
	}
	if _, err := ApplySchemaDiffWithOptions(ctx, db, changes, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := liveTypeOf(t, db, "widgets", "count"); got != "TEXT" {
		t.Fatalf("widgets.count = %q, want TEXT: the rebuild did not run", got)
	}

	var tags int
	if err := db.QueryRow(`SELECT COUNT(*) FROM widget_tags WHERE widget_id = 'w1'`).Scan(&tags); err != nil {
		t.Fatal(err)
	}
	if tags != 1 {
		t.Errorf("widget_tags rows for w1 = %d after the rebuild, want 1 (ON DELETE CASCADE fired)", tags)
	}
	var link *string
	if err := db.QueryRow(`SELECT widget_id FROM widget_notes WHERE id = 'n1'`).Scan(&link); err != nil {
		t.Fatal(err)
	}
	if link == nil || *link != "w1" {
		t.Errorf("widget_notes.widget_id = %v after the rebuild, want w1 (ON DELETE SET NULL fired)", link)
	}

	// Enforcement must be back on for the pool afterwards.
	if err := db.QueryRow(`PRAGMA foreign_keys`).Scan(&fks); err != nil || fks != 1 {
		t.Errorf("foreign_keys = %d (%v) after the rebuild, want 1", fks, err)
	}
}

// TestRebuildRollsBackNewFKViolation: with foreign keys off nothing stops a
// rebuild change set from orphaning a child, so the apply checks before it
// commits. A dangling row that predates the change set does not block it.
func TestRebuildRollsBackNewFKViolation(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	db.SetMaxOpenConns(1)
	for _, s := range []string{
		`CREATE TABLE widgets (id TEXT PRIMARY KEY)`,
		`CREATE TABLE parts (id TEXT PRIMARY KEY, widget_id TEXT REFERENCES widgets(id))`,
		`INSERT INTO widgets (id) VALUES ('w1')`,
		`INSERT INTO parts (id, widget_id) VALUES ('p1', 'w1')`,
		`PRAGMA foreign_keys=OFF`,
		`INSERT INTO parts (id, widget_id) VALUES ('p0', 'gone')`,
		`PRAGMA foreign_keys=ON`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}

	benign := []SchemaChange{{Summary: "touch", SQL: `UPDATE widgets SET id = id`, sqliteRebuildOf: "widgets"}}
	if _, err := ApplySchemaDiffWithOptions(ctx, db, benign, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("a pre-existing dangling row blocked a change set that added none: %v", err)
	}

	orphaning := []SchemaChange{{Summary: "orphan", SQL: `DELETE FROM widgets`, sqliteRebuildOf: "widgets"}}
	if _, err := ApplySchemaDiffWithOptions(ctx, db, orphaning, ApplyOptions{AllowDestructive: true}); err == nil {
		t.Fatal("a change set that orphaned parts.p1 committed")
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM widgets`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("widgets rows = %d after the refused change set, want 1 (rolled back)", n)
	}
}

// TestGenerateRefusesCascadingRebuild: a generated migration runs inside the
// runner's transaction, where PRAGMA foreign_keys cannot change, so its
// rebuild would delete pivot rows. Generate refuses it and names the
// referencing table; a rebuild nothing mutating references still generates.
func TestGenerateRefusesCascadingRebuild(t *testing.T) {
	widgets := func(rels ...entity.Relation) *entity.Entity {
		e := entity.Define("widgets", entity.EntityConfig{
			Name: "widgets", Table: "widgets",
			Fields:    []schema.Field{{Name: "count", Type: schema.Text}},
			Relations: rels,
		}.WithTimestamps(false))
		e.PrimaryKey = "id"
		return e
	}
	tags := entity.Define("tags", entity.EntityConfig{
		Name: "tags", Table: "tags",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	tags.PrimaryKey = "id"
	prev := SchemaSnapshot{Tables: map[string]map[string]string{
		"widgets":     {"id": "TEXT", "count": "INTEGER"},
		"tags":        {"id": "TEXT", "name": "TEXT"},
		"widget_tags": {"widget_id": "TEXT", "tag_id": "TEXT"},
	}}

	pivot := entity.ManyToMany("tags", "tags", "widget_tags", "widget_id", "tag_id")
	_, _, _, err := GeneratePlan(Plan{Registry: secTypeReg{"widgets": widgets(pivot), "tags": tags}}, prev, DialectSQLite)
	if err == nil || !strings.Contains(err.Error(), "widget_tags") {
		t.Fatalf("GeneratePlan error = %v, want a refusal naming widget_tags", err)
	}

	notes := entity.Define("notes", entity.EntityConfig{
		Name: "notes", Table: "notes",
		Fields:    []schema.Field{{Name: "widget_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("widget", "widgets", "widget_id").OnDeleteAction(entity.OnDeleteSetNull)},
	}.WithTimestamps(false))
	notes.PrimaryKey = "id"
	prev.Tables["notes"] = map[string]string{"id": "TEXT", "widget_id": "TEXT"}
	_, _, _, err = GeneratePlan(Plan{Registry: secTypeReg{"widgets": widgets(), "tags": tags, "notes": notes}}, prev, DialectSQLite)
	if err == nil || !strings.Contains(err.Error(), "notes") {
		t.Fatalf("GeneratePlan error = %v, want a refusal naming notes (ON DELETE SET NULL)", err)
	}
	delete(prev.Tables, "notes")

	if _, _, _, err := GeneratePlan(Plan{Registry: secTypeReg{"widgets": widgets(), "tags": tags}}, prev, DialectSQLite); err != nil {
		t.Fatalf("a rebuild with no mutating referrer was refused: %v", err)
	}
}

// TestRetypeRebuildKeepsFKIndex: the rebuild's DROP TABLE takes every index
// with it, so it must recreate the automatic belongs_to index too, not only
// the declared ones. The snapshot lists that index, so a later plan never
// notices it is gone.
func TestRetypeRebuildKeepsFKIndex(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	for _, s := range []string{
		`CREATE TABLE owners (id TEXT PRIMARY KEY)`,
		`CREATE TABLE pets (id TEXT PRIMARY KEY, owner_id TEXT REFERENCES owners(id), count INTEGER)`,
		`CREATE INDEX idx_pets_owner_id ON pets(owner_id)`,
	} {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
	owners := entity.Define("owners", entity.EntityConfig{Name: "owners", Table: "owners"}.WithTimestamps(false))
	owners.PrimaryKey = "id"
	pets := entity.Define("pets", entity.EntityConfig{
		Name: "pets", Table: "pets",
		Fields: []schema.Field{
			{Name: "owner_id", Type: schema.String},
			{Name: "count", Type: schema.Text},
		},
		Relations: []entity.Relation{entity.BelongsTo("owner", "owners", "owner_id")},
	}.WithTimestamps(false))
	pets.PrimaryKey = "id"
	changes, err := DiffSchema(ctx, db, secTypeReg{"owners": owners, "pets": pets})
	if err != nil {
		t.Fatalf("DiffSchema: %v", err)
	}
	if _, err := ApplySchemaDiffWithOptions(ctx, db, changes, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if got := liveTypeOf(t, db, "pets", "count"); got != "TEXT" {
		t.Fatalf("pets.count = %q, want TEXT: the rebuild did not run", got)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'idx_pets_owner_id'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatal("idx_pets_owner_id is gone after the rebuild")
	}
}
