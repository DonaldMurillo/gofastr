package migrate

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestDiffEntityFromLive_TypeChangeDetected proves a column whose declared
// type no longer matches the live DB type SURFACES as a change instead of being
// silently ignored (the pre-fix gap the package doc called "intentionally out
// of scope"). A type change can need a data-specific USING conversion, so it is
// flagged Destructive and refused by default — never silent schema drift.
func TestDiffEntityFromLive_TypeChangeDetected(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "count", Type: schema.Int}, // declared INTEGER
	}, nil, "id")
	changes, err := diffEntityFromLive(ent, nil, DialectSQLite, map[string]string{
		"id":    "TEXT",
		"count": "TEXT", // live holds TEXT — a type change
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var saw bool
	for _, c := range changes {
		if strings.Contains(c.Summary, "count") && strings.Contains(strings.ToLower(c.Summary), "type") {
			saw = true
			if !c.Destructive {
				t.Error("type change must be flagged Destructive")
			}
		}
	}
	if !saw {
		t.Fatalf("type change for count not surfaced (silent drift): %+v", changes)
	}
}

// TestDiffEntityFromLive_NoTypeChangeWhenMatching guards against false
// positives: when declared and live types agree, no type-change change fires.
// (id is declared String→TEXT against live TEXT and must not trip.)
func TestDiffEntityFromLive_NoTypeChangeWhenMatching(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "count", Type: schema.Int},
	}, nil, "id")
	changes, err := diffEntityFromLive(ent, nil, DialectSQLite, map[string]string{
		"id":    "TEXT",
		"count": "INTEGER",
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	for _, c := range changes {
		if strings.Contains(strings.ToLower(c.Summary), "type") {
			t.Errorf("unexpected type-change change when types match: %s", c.Summary)
		}
	}
}

// TestDiffEntityFromLive_RenameColumn proves an explicit rename hint
// (EntityConfig.Renames: old → new) emits a non-destructive RENAME COLUMN
// instead of a data-losing DROP of the old column + ADD of the new one.
// Rename is otherwise indistinguishable from drop+add, so it requires the
// explicit declaration — auto-detection is unsafe.
func TestDiffEntityFromLive_RenameColumn(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "label", Type: schema.String}, // renamed from "name"
	}, nil, "id")
	ent.Config.Renames = map[string]string{"name": "label"}
	changes, err := diffEntityFromLive(ent, nil, DialectSQLite, map[string]string{
		"id":   "TEXT",
		"name": "TEXT", // the old column is still live
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var sawRename bool
	for _, c := range changes {
		s := strings.ToUpper(c.SQL)
		if strings.Contains(s, "RENAME COLUMN") {
			sawRename = true
			if c.Destructive {
				t.Error("rename must NOT be destructive — it preserves data")
			}
		}
		// The rename pair must not ALSO surface as drop+add.
		if strings.Contains(s, "DROP COLUMN") && strings.Contains(c.Summary, "name") {
			t.Error("rename emitted a DROP for the source column instead of a RENAME")
		}
		if strings.Contains(s, "ADD COLUMN") && strings.Contains(c.Summary, "label") {
			t.Error("rename emitted an ADD for the target column instead of a RENAME")
		}
	}
	if !sawRename {
		t.Fatalf("expected a RENAME COLUMN name→label, got: %+v", changes)
	}
}
