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

// TestAdditiveChanges_ExcludesRename pins boot's additive-only contract: the
// boot convergence path (addMissingColumns/additiveChanges) must NOT apply a
// RENAME even though a rename is non-destructive. A rename leaking through
// boot would silently mutate a live column whenever a Renames hint is set —
// and a stale hint could rename the wrong in-use column. Renames belong in a
// reviewable `migrate generate` file, not silent boot convergence.
func TestAdditiveChanges_ExcludesRename(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "label", Type: schema.String},
	}, nil, "id")
	ent.Config.Renames = map[string]string{"oldcol": "label"}
	adds, err := additiveChanges(ent, nil, DialectSQLite, map[string]string{
		"id":     "TEXT",
		"oldcol": "TEXT",
	})
	if err != nil {
		t.Fatalf("additiveChanges: %v", err)
	}
	for _, c := range adds {
		if strings.Contains(strings.ToUpper(c.SQL), "RENAME COLUMN") {
			t.Errorf("boot additive path must be additive-only — it included a rename: %s", c.SQL)
		}
	}
}

// TestAdditiveChanges_NotFooledByRenameInDefault: an ADD COLUMN whose DEFAULT
// string literal contains the words "rename column" must NOT be mistaken for a
// RENAME and discarded at boot. isRenameChange matches the space-delimited
// operation token, not a quote-bordered literal.
func TestAdditiveChanges_NotFooledByRenameInDefault(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "note", Type: schema.String, Default: "rename column"},
	}, nil, "id")
	adds, err := additiveChanges(ent, nil, DialectSQLite, map[string]string{"id": "TEXT"})
	if err != nil {
		t.Fatalf("additiveChanges: %v", err)
	}
	var sawNote bool
	for _, c := range adds {
		if strings.Contains(c.SQL, "note") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Errorf("ADD COLUMN note (DEFAULT 'rename column') was wrongly excluded as a rename: %+v", adds)
	}
}

// TestDiffEntityFromLive_RenameAndTypeChange: when a column is BOTH renamed
// AND retyped, the diff must emit the rename AND the type change. Previously
// the type-change loop looked up the new name (absent from live) and silently
// dropped the type change, leaving the column's type out of sync after the rename.
func TestDiffEntityFromLive_RenameAndTypeChange(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "label", Type: schema.Int}, // renamed from "name" AND retyped (live TEXT)
	}, nil, "id")
	ent.Config.Renames = map[string]string{"name": "label"}
	changes, err := diffEntityFromLive(ent, nil, DialectSQLite, map[string]string{
		"id": "TEXT", "name": "TEXT",
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var sawRename, sawType bool
	for _, c := range changes {
		if strings.Contains(strings.ToUpper(c.SQL), "RENAME COLUMN") {
			sawRename = true
		}
		if strings.Contains(strings.ToLower(c.Summary), "change column label") {
			sawType = true
		}
	}
	if !sawRename {
		t.Error("expected a RENAME COLUMN name→label")
	}
	if !sawType {
		t.Errorf("expected a type change TEXT→INTEGER for the renamed+retyped column; the type change was lost: %+v", changes)
	}
}

// TestDiffEntityFromLive_RawTypeNotFlagged: a column declared with RawType
// (a Postgres domain, custom type, or array) must NOT false-positive as a type
// change every diff — the live DB reports the underlying type, which never
// matches the operator-supplied raw type. RawType is an explicit escape hatch;
// the diff can't verify it, so it skips the comparison.
func TestDiffEntityFromLive_RawTypeNotFlagged(t *testing.T) {
	ent := rawEnt("widgets", "widgets", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "addr", RawType: "email_address"}, // a PG domain over text
	}, nil, "id")
	changes, err := diffEntityFromLive(ent, nil, DialectPostgres, map[string]string{
		"id": "text", "addr": "text", // PG reports the underlying type, not the domain
	})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	for _, c := range changes {
		if strings.Contains(strings.ToLower(c.Summary), "change column addr") {
			t.Errorf("RawType column false-positive as a type change (would fire every diff): %s", c.Summary)
		}
	}
}
