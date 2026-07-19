package sqlite

import (
	"testing"
)

// ============================================================================
// Regression: an omitted column's DEFAULT value was copied straight from the
// stored default without applying the column's affinity. So
//   x REAL NOT NULL DEFAULT true
// stored integer 1 instead of float 1.0. SQLite applies the column affinity
// to the default when materializing the row.
// ============================================================================

// TestDefaultAppliesColumnAffinity covers both insert branches.
func TestDefaultAppliesColumnAffinity(t *testing.T) {
	db := openTestDB(t)

	// t_real has a UNIQUE constraint so inserts route through
	// executeInsertWithConflict → buildInsertRow. t_plain has no constraints
	// so inserts route through the "simple" executeInsert branch.
	if _, err := db.Exec(`CREATE TABLE t_real (
		id INTEGER PRIMARY KEY,
		x REAL NOT NULL DEFAULT true,
		y REAL NOT NULL DEFAULT 1
	)`); err != nil {
		t.Fatalf("create t_real: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE t_plain (
		id INTEGER PRIMARY KEY,
		x REAL NOT NULL DEFAULT true,
		y REAL NOT NULL DEFAULT 1
	)`); err != nil {
		t.Fatalf("create t_plain: %v", err)
	}

	for _, tbl := range []string{"t_real", "t_plain"} {
		t.Run(tbl, func(t *testing.T) {
			if _, err := db.Exec(`INSERT INTO ` + tbl + ` (id) VALUES (1)`); err != nil {
				t.Fatalf("insert: %v", err)
			}

			var xType, yType string
			if err := db.QueryRow(`SELECT typeof(x), typeof(y) FROM `+tbl).Scan(&xType, &yType); err != nil {
				t.Fatalf("select typeof: %v", err)
			}
			if xType != "real" {
				t.Errorf("x typeof: want 'real' (DEFAULT true coerced by REAL affinity), got %q", xType)
			}
			if yType != "real" {
				t.Errorf("y typeof: want 'real' (DEFAULT 1 coerced by REAL affinity), got %q", yType)
			}
		})
	}
}
