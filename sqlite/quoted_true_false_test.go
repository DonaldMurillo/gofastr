package sqlite

import (
	"testing"
)

// ============================================================================
// Regression: a column literally named "true" / "false" (or any other
// TRUE/FALSE spelling reached via a quoted identifier form) must NOT be
// rewritten to an integer literal. Only the *bare* unquoted TRUE/FALSE forms
// are boolean literals (SQLite 3.23+); quoted "..", `..`, and [..] are
// column references no matter what string they contain.
//
// Before the fix, parseIdentOrFunction read only the token VALUE — so a
// quoted "true" was indistinguishable from bare true and was wrongly turned
// into integer 1, breaking round-trips of any column named true/false.
// ============================================================================

// TestQuotedTrueFalseColumnRoundTrips drives the adapter end-to-end: a
// column named "true" stores and returns its declared value, not 1/0.
func TestQuotedTrueFalseColumnRoundTrips(t *testing.T) {
	cases := []struct {
		name   string
		create string // CREATE TABLE statement that declares the column via one of the quoted forms
		open   string // opening quote character
		close  string // matching closing quote character
	}{
		{
			name:   "double quotes",
			create: `CREATE TABLE q("true" INTEGER, "false" INTEGER)`,
			open:   `"`,
			close:  `"`,
		},
		{
			name:   "backticks",
			create: "CREATE TABLE q(`true` INTEGER, `false` INTEGER)",
			open:   "`",
			close:  "`",
		},
		{
			name:   "brackets",
			create: `CREATE TABLE q([true] INTEGER, [false] INTEGER)`,
			open:   "[",
			close:  "]",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db := openTestDB(t)

			if _, err := db.Exec(tc.create); err != nil {
				t.Fatalf("create: %v", err)
			}

			// Insert distinct non-boolean values into the true/false-named
			// columns using the SAME quoted form the table was declared with.
			stmt := "INSERT INTO q(" + tc.open + "true" + tc.close + ", " + tc.open + "false" + tc.close + ") VALUES (7, 9)"
			if _, err := db.Exec(stmt); err != nil {
				t.Fatalf("insert: %v", err)
			}

			// Read back via the same quoted form.
			sel := "SELECT " + tc.open + "true" + tc.close + ", " + tc.open + "false" + tc.close + " FROM q"
			var trueVal, falseVal int64
			if err := db.QueryRow(sel).Scan(&trueVal, &falseVal); err != nil {
				t.Fatalf("select: %v", err)
			}
			if trueVal != 7 {
				t.Errorf(`"true" column: want 7, got %d`, trueVal)
			}
			if falseVal != 9 {
				t.Errorf(`"false" column: want 9, got %d`, falseVal)
			}
		})
	}
}

// TestBareTrueFalseStillLiteral confirms that the fix did not regress the
// intended bare-true/false → integer 1/0 rewrite (it must still work).
func TestBareTrueFalseStillLiteral(t *testing.T) {
	db := openTestDB(t)

	if _, err := db.Exec(`CREATE TABLE b (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO b (id) VALUES (1)`); err != nil {
		t.Fatalf("insert: %v", err)
	}

	cases := []struct {
		expr string
		want int64
	}{
		{"SELECT true FROM b", 1},
		{"SELECT false FROM b", 0},
		{"SELECT TRUE FROM b", 1},
		{"SELECT FALSE FROM b", 0},
	}
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			var got int64
			if err := db.QueryRow(tc.expr).Scan(&got); err != nil {
				t.Fatalf("query %q: %v", tc.expr, err)
			}
			if got != tc.want {
				t.Errorf("query %q: want %d, got %d", tc.expr, tc.want, got)
			}
		})
	}
}
