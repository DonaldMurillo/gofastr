package schema

import "testing"

// DecimalExactOnSQLite follows SQLite's affinity rules: TEXT and BLOB
// columns keep the text, every other column keeps 15 significant digits
// of a value with a fraction and any whole number that fits 64 bits.
func TestDecimalExactOnSQLite(t *testing.T) {
	for _, c := range []struct {
		col, v string
		want   bool
	}{
		{"", "123456789012345.1", false},
		{"", "12345678901234.5", true},
		{"", "999999999999999.1234", false},
		{"", "0.000000000000001234", true},
		{"", "123456789012345", true},
		{"NUMERIC(30,0)", "123456789012345678", true},
		{"NUMERIC(30,0)", "1234567890123456789", false},
		{"", "-1.00000000000000", true},
		{"", "1.2e-3", true},
		{"TEXT", "999999999999999.1234", true},
		{"varchar(40)", "999999999999999.1234", true},
		{"BLOB", "999999999999999.1234", true},
		{"BIGINT", "999999999999999.1234", false},
		{"POINT", "999999999999999.1234", false},
	} {
		if got := DecimalExactOnSQLite(c.col, c.v); got != c.want {
			t.Errorf("DecimalExactOnSQLite(%q, %q) = %v, want %v", c.col, c.v, got, c.want)
		}
	}
}
