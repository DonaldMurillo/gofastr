package migrate

import (
	"slices"
	"testing"
)

// Dropped tables are keyed by an ASCII-only fold, the folding Postgres
// applies to an unquoted identifier. Under strings.ToLower, "kiosk" and
// the KELVIN SIGN spelling "Kiosk" shared one key, and the drop plan
// lost one of the two tables.
func TestDropOrderKeepsLookAlikeTables(t *testing.T) {
	tables := []string{"kiosk", "Kiosk"}
	got := sortDroppedTables(DialectSQLite, tables, map[string]string{})
	slices.Sort(got)
	want := slices.Clone(tables)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("drop plan = %q, want both tables %q", got, want)
	}
}
