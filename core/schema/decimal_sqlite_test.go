package schema

import "testing"

// DecimalExactOnSQLite follows SQLite's affinity precedence: TEXT and BLOB
// keep text, INTEGER and NUMERIC keep signed-int64 literals, and REAL
// always uses a float with the precision and magnitude guards.
func TestDecimalExactOnSQLite(t *testing.T) {
	for _, sample := range []struct {
		column, literal string
		want            bool
	}{
		{"", "123456789012345.1", false},
		{"", "12345678901234.5", true},
		{"", "999999999999999.1234", false},
		{"", "0.000000000000001234", true},
		{"", "123456789012345", true},
		{"NUMERIC(30,0)", "123456789012345678", true},
		{"NUMERIC(30,0)", "1234567890123456789", true},
		{"NUMERIC(30,0)", "9223372036854775807", true},
		{"NUMERIC(30,0)", "-9223372036854775808", true},
		{"NUMERIC(30,0)", "+9223372036854775807", true},
		{"NUMERIC(30,0)", "9223372036854775808", false},
		{"NUMERIC(30,0)", "-9223372036854775809", false},
		{"NUMERIC(30,0)", "10000000000000000000", true},
		{"NUMERIC(30,0)", "1234567890123456789.0", false},
		{"NUMERIC(30,0)", "123456789012345678.0", false},
		{"NUMERIC(30,0)", "1e18", true},
		{"REAL", "9223372036854775807", false},
		{"REAL", "-9223372036854775808", false},
		{"REAL", "1234567890123456789", false},
		{"float", "9007199254740993", false},
		{"DOUBLE PRECISION", "9007199254740993", false},
		{"REAL", "123456789012345", true},
		{"REAL", "1e-400", false},
		{"REAL", "0e-400", true},
		{"FLOATING POINT", "9223372036854775807", true},
		{"REALINT", "9223372036854775807", true},
		{"REAL TEXT", "9223372036854775807", true},
		{"BLOB REAL", "9223372036854775807", true},
		{"", "-1.00000000000000", true},
		{"", "1.2e-3", true},
		{"NUMERIC", "1e-400", false},
		{"NUMERIC", "-1e-400", false},
		{"NUMERIC", "1e-320", false},
		{"NUMERIC", "1e-307", true},
		{"NUMERIC", "0e-400", true},
		{"NUMERIC", "-0.0", true},
		{"NUMERIC", "0.0000000000000000001", true},
		{"TEXT", "1e-400", true},
		{"TEXT", "999999999999999.1234", true},
		{"varchar(40)", "999999999999999.1234", true},
		{"BLOB", "999999999999999.1234", true},
		{"BIGINT", "999999999999999.1234", false},
		{"POINT", "999999999999999.1234", false},
	} {
		if got := DecimalExactOnSQLite(sample.column, sample.literal); got != sample.want {
			t.Errorf("DecimalExactOnSQLite(%q, %q) = %v, want %v", sample.column, sample.literal, got, sample.want)
		}
	}
}

// A float that would drop digits and one that would underflow are
// refused for different reasons, so the 400 names the right limit.
func TestSQLiteDecimalRefusalReason(t *testing.T) {
	for _, sample := range []struct {
		literal string
		reason  string
		param   string
		value   any
	}{
		{"9223372036854775808", ReasonDecimalSignificantDigits.Code(), "max", SQLiteDecimalDigits},
		{"1e-400", ReasonDecimalUnderflow.Code(), "min", "1e-307"},
	} {
		violation, refused := SQLiteDecimalRefusal("NUMERIC", sample.literal)
		if !refused || violation.Reason.Code() != sample.reason || violation.Params[sample.param] != sample.value {
			t.Errorf("SQLiteDecimalRefusal(%q) = %v %v %v, want %s %s=%v", sample.literal, refused, violation.Reason.Code(), violation.Params, sample.reason, sample.param, sample.value)
		}
	}
}
