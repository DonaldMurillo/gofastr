package filter

import (
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestCoerceValueRefusesValuesPostgresCannotBind pins CoerceValue's
// promise ("a value that is not of the column's type is a 400 naming the
// field on both dialects") for values Postgres refuses at bind time while
// SQLite compares them and answers 200: text that is not valid UTF-8 or
// holds a NUL (22021), a Decimal exponent past ±1000 (numeric_in, 22P02)
// and a year-0 Timestamp or Date (Postgres has no year 0, 22008).
func TestCoerceValueRefusesValuesPostgresCannotBind(t *testing.T) {
	for _, tc := range []struct {
		t   schema.FieldType
		raw string
	}{
		{schema.String, "a\xffb"},
		{schema.String, "a\x00b"},
		{schema.Text, "\xc3"},
		{schema.Enum, "x\x00"},
		{schema.UUID, "\xff"},
		{schema.Relation, "a\x00"},
		{schema.Decimal, "1e1001"},
		{schema.Decimal, "1e-1001"},
		{schema.Decimal, "1e99999999999999999999"},
		{schema.Timestamp, "0000-01-01"},
		{schema.Timestamp, "0000-12-31T23:00:00Z"},
		{schema.Timestamp, "0001-01-01T00:30:00+01:00"}, // year 0 in UTC
		{schema.Date, "0000-01-01"},
	} {
		if _, err := CoerceValue("f", tc.t, tc.raw); err == nil {
			t.Errorf("CoerceValue(%s, %q) accepted; Postgres refuses it at bind (a 500)", fieldTypeName(tc.t), tc.raw)
		}
	}
	// The edges Postgres takes still pass.
	for _, tc := range []struct {
		t   schema.FieldType
		raw string
	}{
		{schema.String, "héllo ✓"},
		{schema.Decimal, "1e1000"},
		{schema.Decimal, "-2.5E-1000"},
		{schema.Timestamp, "0001-01-01"},
		{schema.Date, "0001-01-01"},
	} {
		if _, err := CoerceValue("f", tc.t, tc.raw); err != nil {
			t.Errorf("CoerceValue(%s, %q): %v", fieldTypeName(tc.t), tc.raw, err)
		}
	}

	// Every surface that reaches the bind refuses it, a _like substring
	// (never coerced, still a text bind) included.
	fields := []schema.Field{{Name: "title", Type: schema.String}}
	for _, raw := range []string{"title=a%FFb", "title_like=a%00b", "title_in=ok,a%FFb"} {
		q, _ := url.ParseQuery(raw)
		if _, err := ParseFiltersValues(q, fields); err == nil {
			t.Errorf("?%s accepted by ParseFiltersValues", raw)
		}
	}
	for _, raw := range []string{
		`{"field":"title","value":"a\u0000b"}`,
		`{"field":"title","op":"like","value":"a\u0000b"}`,
		`{"field":"title","op":"in","values":["a","b\u0000"]}`,
	} {
		if _, err := ParseWhere(raw, fields); err == nil {
			t.Errorf("?where=%s accepted", raw)
		}
	}
	if _, err := ValidatePredicate(&Predicate{Field: "title", Op: OpLike, Value: "\xff"}, fields); err == nil {
		t.Errorf("ValidatePredicate accepted a like leaf holding invalid UTF-8")
	}
}
