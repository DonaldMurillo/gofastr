package migrate

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins the SQLite column-type-change false convergence, found by the 2026-09-04
// red-probe round; fixed in schema_diff.go by rendering a real table rebuild
// (create → copy → drop → rename → recreate declared indices) for SQLite
// retypes at both apply and generate time instead of a SQL comment.
// Family: F14 migration and schema safety
// Property: a schema change that is reported as applied (or committed into a
// versioned migration file with an advancing snapshot) must actually change
// the schema; a dialect that cannot express the change must fail loud, never
// report false convergence.
// Surfaces: framework/migrate/schema_diff.go::diffEntityFromLive (collects
// SQLite retypes), framework/migrate/schema_diff.go::sqliteRebuildChange
// (renders the rebuild DDL), framework/migrate/schema_diff.go::
// ApplySchemaDiffWithOptions (executes the rebuild, counts it applied),
// framework/migrate/snapshot.go::GeneratePlan (emits the rebuild as the Up of
// a committed migration and advances the snapshot past it),
// framework/migrate/generate_file.go::GenerateMigrationFile (writes that file
// to disk and saves the advanced snapshot).

// secTypeReg is a minimal entity.Registry for the tests below.
type secTypeReg map[string]*entity.Entity

func (r secTypeReg) All() map[string]*entity.Entity { return r }

func (r secTypeReg) AllSorted() []*entity.Entity {
	out := make([]*entity.Entity, 0, len(r))
	for _, e := range r {
		out = append(out, e)
	}
	return out
}

func (r secTypeReg) Get(name string) (*entity.Entity, error) {
	if e, ok := r[name]; ok {
		return e, nil
	}
	return nil, errSecTypeNotFound
}

type secTypeNotFound struct{}

var errSecTypeNotFound = secTypeNotFound{}

func (secTypeNotFound) Error() string { return "entity not found" }

// widgetsTypeReg builds the drifted declaration through the JSON declaration
// path (no core/schema import): live table has count INTEGER, the entity
// declares count TEXT.
func widgetsTypeReg(t *testing.T) secTypeReg {
	t.Helper()
	var decl entity.EntityDeclaration
	if err := json.Unmarshal([]byte(`{
		"name": "widgets", "table": "widgets",
		"fields": [{"name": "id", "type": "string"}, {"name": "count", "type": "text"}]
	}`), &decl); err != nil {
		t.Fatalf("declaration: %v", err)
	}
	cfg, err := decl.Config()
	if err != nil {
		t.Fatalf("declaration config: %v", err)
	}
	e := &entity.Entity{Config: cfg}
	e.PrimaryKey = "id"
	return secTypeReg{"widgets": e}
}

// liveTypeOf reads one column's live SQLite type.
func liveTypeOf(t *testing.T, db *sql.DB, table, col string) string {
	t.Helper()
	live, err := ReadLiveColumnsSQLite(context.Background(), db, table)
	if err != nil {
		t.Fatalf("read live columns: %v", err)
	}
	typ, ok := live[col]
	if !ok {
		t.Fatalf("column %s vanished from %s", col, table)
	}
	return typ
}

// TestTypeChangeApplyChangesSchema: the AllowDestructive opt-in is the
// documented "actually perform it" path; on SQLite the rebuild must converge
// the column type. Reporting success while the column keeps its old type is
// false convergence.
func TestTypeChangeApplyChangesSchema(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	if _, err := db.Exec(`CREATE TABLE widgets (id TEXT PRIMARY KEY, count INTEGER NOT NULL DEFAULT 0)`); err != nil {
		t.Fatalf("create live table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, count) VALUES ('w1', 7)`); err != nil {
		t.Fatalf("seed live row: %v", err)
	}

	changes, err := DiffSchema(ctx, db, widgetsTypeReg(t))
	if err != nil {
		t.Fatalf("DiffSchema: %v", err)
	}
	var sawTypeChange bool
	for _, c := range changes {
		if strings.Contains(c.Summary, "change column") {
			sawTypeChange = true
			if !c.Destructive {
				t.Fatalf("type change not marked destructive: %+v", c)
			}
		}
	}
	if !sawTypeChange {
		t.Fatalf("diff produced no type change: %+v", changes)
	}

	if _, err := ApplySchemaDiffWithOptions(ctx, db, changes, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("apply refused an opted-in SQLite type change: %v", err)
	}

	got := liveTypeOf(t, db, "widgets", "count")
	if got != "TEXT" {
		t.Fatalf("SECURITY: [migrate] ApplySchemaDiffWithOptions(AllowDestructive) returned success for the "+
			"widgets.count type change but the live column is still %q — the apply must converge the "+
			"column (SQLite table rebuild) or fail loud, never report success over an unchanged schema", got)
	}
	// The rebuild copies rows (INSERT ... SELECT), it does not drop them:
	// convergence that loses the table's data is not convergence.
	var countVal string
	if err := db.QueryRow(`SELECT count FROM widgets WHERE id = 'w1'`).Scan(&countVal); err != nil {
		t.Fatalf("row lost to the rebuild: %v", err)
	}
	if countVal != "7" {
		t.Fatalf("row value not carried across the rebuild: got %q, want %q", countVal, "7")
	}
}

// TestTypeChangeGeneratedUpIsExecutable: the offline generator must not commit
// a migration whose entire Up is a comment while advancing the snapshot past
// the change. The Up must carry the real rebuild DDL.
func TestTypeChangeGeneratedUpIsExecutable(t *testing.T) {
	prev := SchemaSnapshot{
		Tables: map[string]map[string]string{
			"widgets": {"id": "TEXT", "count": "INTEGER"},
		},
	}
	up, _, next, err := GeneratePlan(Plan{Registry: widgetsTypeReg(t)}, prev, DialectSQLite)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.TrimSpace(up) == "" {
		t.Fatalf("type change against the snapshot produced no Up SQL at all")
	}
	for _, line := range strings.Split(up, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "--") {
			continue
		}
		return // found a line of real DDL: the Up is executable
	}
	t.Fatalf("SECURITY: [migrate] GeneratePlan committed a SQLite column type change whose entire Up "+
		"SQL is a comment (%q) and advanced the snapshot to count=%q — the migration file applies as a "+
		"no-op while schema.snapshot.json claims the type change landed, masking the drift from every "+
		"future generate run", up, next.Tables["widgets"]["count"])
}

// TestSQLiteRebuildKeepsRowsManagedColsIndex pins the rebuild's fidelity
// claims on a mixed table: sibling columns and their data survive,
// framework-managed columns the entity doesn't declare are preserved (the
// DROP guard's posture), and declared indices are recreated after the rename.
func TestSQLiteRebuildKeepsRowsManagedColsIndex(t *testing.T) {
	ctx := context.Background()
	db := openMigrateSQLite(t)
	if _, err := db.Exec(`CREATE TABLE widgets (
		id TEXT PRIMARY KEY,
		count INTEGER NOT NULL DEFAULT 0,
		title TEXT,
		created_at TIMESTAMP
	)`); err != nil {
		t.Fatalf("create live table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO widgets (id, count, title, created_at) VALUES ('w1', 7, 'kept', '2026-01-01 00:00:00')`); err != nil {
		t.Fatalf("seed live row: %v", err)
	}
	if _, err := db.Exec(`CREATE INDEX idx_widgets_title ON widgets(title)`); err != nil {
		t.Fatalf("create live index: %v", err)
	}

	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "count", Type: schema.Text}, // the retype: INTEGER → TEXT
		{Name: "title", Type: schema.String},
	}, nil, "id")
	ts := true
	ent.Config.Timestamps = &ts // created_at is framework-managed, not declared
	ent.Config.Indices = []entity.Index{{Name: "idx_widgets_title", Columns: []string{"title"}}}
	reg := secTypeReg{"widgets": ent}

	changes, err := DiffSchema(ctx, db, reg)
	if err != nil {
		t.Fatalf("DiffSchema: %v", err)
	}
	if _, err := ApplySchemaDiffWithOptions(ctx, db, changes, ApplyOptions{AllowDestructive: true}); err != nil {
		t.Fatalf("apply rebuild: %v", err)
	}

	live, err := ReadLiveColumnsSQLite(ctx, db, "widgets")
	if err != nil {
		t.Fatalf("read live columns: %v", err)
	}
	if live["count"] != "TEXT" {
		t.Fatalf("count not retyped: %+v", live)
	}
	for _, col := range []string{"id", "title", "created_at"} {
		if _, ok := live[col]; !ok {
			t.Fatalf("column %s lost to the rebuild: %+v", col, live)
		}
	}
	var title, createdAt string
	if err := db.QueryRow(`SELECT title, CAST(created_at AS TEXT) FROM widgets WHERE id = 'w1'`).Scan(&title, &createdAt); err != nil {
		t.Fatalf("row lost to the rebuild: %v", err)
	}
	if title != "kept" || !strings.HasPrefix(createdAt, "2026-01-01") {
		t.Fatalf("row data not carried across the rebuild: title=%q created_at=%q", title, createdAt)
	}
	var idxN int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_widgets_title'`).Scan(&idxN); err != nil {
		t.Fatalf("count indexes: %v", err)
	}
	if idxN != 1 {
		t.Fatalf("declared index not recreated after the rebuild (count=%d)", idxN)
	}
}
