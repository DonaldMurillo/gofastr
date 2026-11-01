package filter

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// score=5&score_gt=3 used to keep or drop the equality depending on which
// key the map handed over first: a suffixed key marked the column
// "consumed" and a later plain key on it was dropped. Both predicates are
// kept now (they AND), in sorted key order, so every parse of the same
// query is the same filter list.
func TestParseFiltersIsDeterministic(t *testing.T) {
	fields := []schema.Field{
		{Name: "score", Type: schema.Int},
		{Name: "status", Type: schema.String},
		{Name: "tag", Type: schema.String},
	}
	q := url.Values{
		"score": {"5"}, "score_gt": {"3"}, "status_in": {"a,b"},
		"status": {"a"}, "tag_like": {"x"}, "tag_ne": {"y"},
	}
	first, err := ParseFiltersValues(q, fields)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for i := range 200 {
		got, err := ParseFiltersValues(q, fields)
		if err != nil {
			t.Fatalf("parse %d: %v", i, err)
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("parse %d differs:\n got %v\nwant %v", i, got, first)
		}
	}
	var eq, gt bool
	for _, f := range first {
		if f.Field == "score" && f.Op == OpEq {
			eq = true
		}
		if f.Field == "score" && f.Op == OpGt {
			gt = true
		}
	}
	if !eq || !gt {
		t.Fatalf("score=5&score_gt=3 kept eq=%v gt=%v, want both: %v", eq, gt, first)
	}
}

// CoerceValue is the one check every filter surface runs on a value.
func TestCoerceValueByType(t *testing.T) {
	ok := []struct {
		t    schema.FieldType
		raw  string
		want any
	}{
		{schema.Int, "42", int64(42)},
		{schema.Int, "-7", int64(-7)},
		{schema.Float, "1.5", 1.5},
		{schema.Float, "2e3", 2000.0},
		{schema.Decimal, "1.2500", "1.2500"},
		{schema.Bool, "true", true},
		{schema.Bool, "0", false},
		{schema.Bool, "T", true},
		// Every Timestamp binds in the layout crud stores (TimestampLayout),
		// so SQLite's text comparison and Postgres's timestamptz agree.
		{schema.Timestamp, "2026-10-27T10:00:00Z", "2026-10-27T10:00:00.000000Z"},
		{schema.Timestamp, "2026-10-27T10:00:00.123456+02:00", "2026-10-27T08:00:00.123456Z"},
		{schema.Timestamp, "2026-10-27T10:00:00.1234567Z", "2026-10-27T10:00:00.123456Z"},
		// A bare date is its midnight, UTC.
		{schema.Timestamp, "2026-10-27", "2026-10-27T00:00:00.000000Z"},
		// What an <input type=datetime-local> sends, and SQLite's
		// datetime() spelling: a time with no zone is read as UTC, the
		// zone every stored Timestamp is in.
		{schema.Timestamp, "2026-10-27T10:00", "2026-10-27T10:00:00.000000Z"},
		{schema.Timestamp, "2026-10-27T10:00:30.5", "2026-10-27T10:00:30.500000Z"},
		{schema.Timestamp, "2026-10-27 10:00:30", "2026-10-27T10:00:30.000000Z"},
		{schema.Date, "2026-10-27", "2026-10-27"},
		{schema.String, "anything", "anything"},
		{schema.UUID, "not-a-uuid", "not-a-uuid"},
	}
	for _, c := range ok {
		got, err := CoerceValue("f", c.t, c.raw)
		if err != nil || got != c.want {
			t.Errorf("CoerceValue(%s, %q) = %#v, %v; want %#v", fieldTypeName(c.t), c.raw, got, err, c.want)
		}
	}
	bad := []struct {
		t   schema.FieldType
		raw string
	}{
		{schema.Int, "abc"}, {schema.Int, "1.5"}, {schema.Int, "1e3"}, {schema.Int, ""},
		{schema.Int, "0x10"}, {schema.Int, "1_000"},
		{schema.Float, "abc"}, {schema.Float, "NaN"}, {schema.Float, "Inf"}, {schema.Float, "0x1p-2"},
		{schema.Float, "1_0"},
		{schema.Decimal, "abc"}, {schema.Decimal, "Infinity"}, {schema.Decimal, "0x10"},
		{schema.Bool, "yes"}, {schema.Bool, "on"}, {schema.Bool, ""},
		{schema.Timestamp, "yesterday"}, {schema.Timestamp, "2026-13-01"},
		{schema.Date, "2026-02-30"}, {schema.Date, "2026-10-27T00:00:00Z"},
	}
	for _, c := range bad {
		if got, err := CoerceValue("f", c.t, c.raw); err == nil {
			t.Errorf("CoerceValue(%s, %q) = %#v, want an error", fieldTypeName(c.t), c.raw, got)
		} else if !strings.Contains(err.Error(), `"f"`) {
			t.Errorf("CoerceValue(%s, %q) error %q does not name the field", fieldTypeName(c.t), c.raw, err)
		}
	}
}

// A Timestamp filter binds the stored layout, not the text sent: SQLite
// compares the column as text.
func TestTimestampFilterBindsStoredLayout(t *testing.T) {
	fields := []schema.Field{{Name: "at", Type: schema.Timestamp}}
	got, err := ParseFiltersValues(url.Values{"at": {"2026-10-27T10:00:00Z"}}, fields)
	if err != nil || len(got) != 1 {
		t.Fatalf("parse = %v, %v", got, err)
	}
	if v := got[0].BindValue(); v != "2026-10-27T10:00:00.000000Z" {
		t.Errorf("BindValue = %#v, want the stored layout", v)
	}
	if v := TypedBind(schema.Timestamp, "2026-10-27"); v != "2026-10-27T00:00:00.000000Z" {
		t.Errorf("TypedBind = %#v, want the stored layout", v)
	}
}

// Every value surface refuses a mistyped value: the flat parser (scalar and
// IN), ?where= and a Go-built tree through ValidatePredicate.
func TestValueSurfacesRefuseWrongType(t *testing.T) {
	fields := []schema.Field{{Name: "n", Type: schema.Int}, {Name: "on", Type: schema.Bool}}
	for _, q := range []string{"n=abc", "n_gt=1.5", "n_in=1,abc", "on=yes", "on_in=true,nope"} {
		v, _ := url.ParseQuery(q)
		if _, err := ParseFiltersValues(v, fields); err == nil {
			t.Errorf("ParseFiltersValues(%s) accepted a mistyped value", q)
		}
	}
	// Lenient drops unknown keys; it does not bind a mistyped value.
	v, _ := url.ParseQuery("n=abc")
	if _, err := ParseFiltersValues(v, fields, Lenient()); err == nil {
		t.Errorf("Lenient ParseFiltersValues(n=abc) accepted a mistyped value")
	}
	for _, js := range []string{
		`{"field":"n","value":"abc"}`,
		`{"field":"n","op":"in","values":["1","x"]}`,
		`{"or":[{"field":"on","value":"true"},{"field":"on","value":"yes"}]}`,
	} {
		if _, err := ParseWhere(js, fields); err == nil {
			t.Errorf("ParseWhere(%s) accepted a mistyped value", js)
		}
	}
	if _, err := ValidatePredicate(&Predicate{Field: "n", Op: OpLt, Value: "x"}, fields); err == nil {
		t.Errorf("ValidatePredicate accepted n < x")
	}
	// A well-typed tree binds typed values.
	p, err := ValidatePredicate(&Predicate{Field: "n", Op: OpIn, Values: []string{"1", "2"}}, fields)
	if err != nil {
		t.Fatalf("ValidatePredicate: %v", err)
	}
	if c := BuildPredicate(p); fmt.Sprint(c.Args) != "[1 2]" || reflect.TypeOf(c.Args[0]) != reflect.TypeFor[int64]() {
		t.Fatalf("args = %#v, want int64 binds", c.Args)
	}
}

// _like folds case on both sides, the way ?q= search does, and keeps the
// literal-wildcard escape.
func TestLikeClauseFoldsCase(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}
	v, _ := url.ParseQuery("status_like=" + url.QueryEscape("OP%"))
	fs, err := ParseFiltersValues(v, fields)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	sql, args := LikeClause("status", 1, fs[0].Value)
	if sql != `LOWER(status) LIKE $1 ESCAPE '\'` || args != `%op\%%` {
		t.Fatalf("LikeClause = %q, %q", sql, args)
	}
	p, err := ParseWhere(`{"field":"status","op":"like","value":"OP"}`, fields)
	if err != nil {
		t.Fatalf("where: %v", err)
	}
	if c := BuildPredicate(p); c.SQL != `(LOWER(status) LIKE $1 ESCAPE '\')` || c.Args[0] != "%op%" {
		t.Fatalf("where like = %q %v", c.SQL, c.Args)
	}
}

// Go parses year 0000; Postgres refuses it ("date/time field value out
// of range"), so a filter of 0000-01-01 answered 500 there and 200 on
// SQLite (bug hunt r7 R7-076). It is refused as a value of the wrong
// type, and so is a timestamp whose UTC instant falls in year 0.
func TestYearZeroFilterRefused(t *testing.T) {
	for _, c := range []struct {
		t   schema.FieldType
		raw string
	}{
		{schema.Date, "0000-01-01"},
		{schema.Timestamp, "0000-06-01T00:00:00Z"},
		{schema.Timestamp, "0000-06-01"},
		{schema.Timestamp, "0001-01-01T00:30:00+01:00"},
	} {
		if v, err := CoerceValue("at", c.t, c.raw); err == nil {
			t.Errorf("CoerceValue(%s, %q) = %#v, want refused", fieldTypeName(c.t), c.raw, v)
		}
	}
	if _, err := CoerceValue("at", schema.Date, "0001-01-01"); err != nil {
		t.Errorf("year 1 date refused: %v", err)
	}
}
