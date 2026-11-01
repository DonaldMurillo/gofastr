package schema

import "testing"

// decimalCode is the violation code Validate answers for value on f,
// or "" when it accepts the value.
func decimalCode(t *testing.T, f Field, value string) string {
	t.Helper()
	err := Validate(f, value)
	if err == nil {
		return ""
	}
	return AsViolation(err).Code()
}

// A Decimal exponent past ±1000 is a literal Postgres numeric_in refuses
// (22P02) at write time, while ParseFloat reads "1e-1001" as 0 with no
// error and SQLite stores it. The refusal is textual, ahead of big.Rat,
// so an exponent in the millions never builds a power of ten.
func TestDecimalExponentPastPostgresRangeRefused(t *testing.T) {
	for _, f := range []Field{
		{Name: "amount", Type: Decimal},
		{Name: "amount", Type: Decimal, RawType: "NUMERIC"},
		{Name: "amount", Type: Decimal, RawType: "TEXT"},
	} {
		for _, s := range []string{
			"1e-1001", "1e1001", "0e-1001", "-1E+1001", ".5e-1001",
			"1e-999999", "1e-999999999", "1e-99999999999999999999",
		} {
			if got := decimalCode(t, f, s); got != "decimal_exponent" {
				t.Errorf("RawType %q: %q answered %q, want decimal_exponent", f.RawType, s, got)
			}
		}
	}
	// The boundary itself is a literal Postgres reads; an unconstrained
	// NUMERIC column stores it. (A positive exponent past float64's range,
	// 1e1000, stays refused as not_decimal, as it always was.)
	open := Field{Name: "amount", Type: Decimal, RawType: "NUMERIC"}
	for _, s := range []string{"1e-1000", "1e-0001000", "1e300"} {
		if got := decimalCode(t, open, s); got != "" {
			t.Errorf("NUMERIC: %q answered %q, want accepted", s, got)
		}
	}
}

// The default Decimal column is DECIMAL(19,4): 15 integer digits.
// Postgres refuses a 16th (22003) while SQLite's numeric affinity stores
// it, so the create answered 500 on one dialect and 201 on the other.
func TestDecimalIntegerDigitsPastColumnRefused(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal}
	for _, s := range []string{
		"1e16", "1e15", "12345678901234567", "1000000000000000",
		"-1000000000000000", "0.0000000001e25", "1000000000000000.0000",
	} {
		if got := decimalCode(t, f, s); got != "decimal_integer_digits" {
			t.Errorf("%q answered %q, want decimal_integer_digits", s, got)
		}
	}
	for _, s := range []string{
		"999999999999999", "999999999999999.9999", "-999999999999999.9999",
		"1e14", "000000000000000000001", "0", "-0", "0.0", "+12.5",
	} {
		if got := decimalCode(t, f, s); got != "" {
			t.Errorf("%q answered %q, want accepted", s, got)
		}
	}
}

// Postgres rounds a fifth fraction digit away at DECIMAL(19,4) while
// SQLite stores the value as written, so the two dialects kept different
// numbers. A value with more fraction digits than the column's scale is
// refused; trailing zeros are not digits the column loses.
func TestDecimalFractionDigitsPastColumnRefused(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal}
	for _, s := range []string{
		"1.23456", "0.00001", "1e-5", "1.5e-4", "-0.12345", "12345.678901",
	} {
		if got := decimalCode(t, f, s); got != "decimal_fraction_digits" {
			t.Errorf("%q answered %q, want decimal_fraction_digits", s, got)
		}
	}
	for _, s := range []string{
		"1.2345", "1.23450000", "12345e-4", "0.0001", "1e-4", "00001.1000", "5.",
	} {
		if got := decimalCode(t, f, s); got != "" {
			t.Errorf("%q answered %q, want accepted", s, got)
		}
	}
}

// A RawType of DECIMAL(p,s) or NUMERIC(p,s) is the column, so its own
// precision and scale are the bound, not the default's.
func TestDecimalRawTypePrecisionIsTheColumn(t *testing.T) {
	cases := []struct {
		raw, value, want string
	}{
		{"NUMERIC(10,2)", "12345678.12", ""},
		{"NUMERIC(10,2)", "123456789.1", "decimal_integer_digits"},
		{"NUMERIC(10,2)", "1.123", "decimal_fraction_digits"},
		{"numeric( 5 )", "12345", ""},
		{"numeric( 5 )", "123456", "decimal_integer_digits"},
		{"numeric( 5 )", "1.5", "decimal_fraction_digits"},
		{"decimal(30, 10)", "12345678901234567890.1234567891", ""},
		{"decimal(30, 10)", "1.12345678901", "decimal_fraction_digits"},
		{"DECIMAL(4,4)", "0.1234", ""},
		{"DECIMAL(4,4)", "1", "decimal_integer_digits"},
		// An unconstrained NUMERIC and a non-numeric RawType bound only
		// the exponent.
		{"NUMERIC", "1.123456789012345678901234567890", ""},
		{"NUMERIC", "1e300", ""},
		{"TEXT", "1.123456789", ""},
	}
	for _, c := range cases {
		f := Field{Name: "amount", Type: Decimal, RawType: c.raw}
		if got := decimalCode(t, f, c.value); got != c.want {
			t.Errorf("RawType %q: %q answered %q, want %q", c.raw, c.value, got, c.want)
		}
	}
}

// The refusal names the column's limit, so the 400 says what fits.
func TestDecimalColumnViolationParams(t *testing.T) {
	f := Field{Name: "amount", Type: Decimal}
	v := AsViolation(Validate(f, "1.23456"))
	if v.Params["max"] != 4 {
		t.Errorf("fraction params = %v, want max 4", v.Params)
	}
	v = AsViolation(Validate(f, "1e16"))
	if v.Params["max"] != 15 {
		t.Errorf("integer params = %v, want max 15", v.Params)
	}
	v = AsViolation(Validate(f, "1e-1001"))
	if v.Params["max"] != MaxDecimalExponent {
		t.Errorf("exponent params = %v, want max %d", v.Params, MaxDecimalExponent)
	}
}

// DecimalColumn reads the precision and scale the migrator gives the
// field's column.
func TestDecimalColumn(t *testing.T) {
	cases := []struct {
		raw     string
		p, s    int
		bounded bool
	}{
		{"", 19, 4, true},
		{"NUMERIC(10,2)", 10, 2, true},
		{" Decimal ( 8 ) ", 8, 0, true},
		{"NUMERIC", 0, 0, false},
		{"NUMERIC(3,5)", 0, 0, false},
		{"NUMERIC(0)", 0, 0, false},
		{"NUMERIC(1001)", 0, 0, false},
		{"NUMERIC(10,-2)", 0, 0, false},
		{"TEXT", 0, 0, false},
		{"NUMERIC(10,2) NOT NULL", 0, 0, false},
	}
	for _, c := range cases {
		p, s, ok := DecimalColumn(Field{Type: Decimal, RawType: c.raw})
		if p != c.p || s != c.s || ok != c.bounded {
			t.Errorf("DecimalColumn(%q) = %d, %d, %v; want %d, %d, %v", c.raw, p, s, ok, c.p, c.s, c.bounded)
		}
	}
}
