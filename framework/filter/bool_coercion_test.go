package filter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestBoolFilterValuesBindAsBools covers the query-string value-coercion
// path for Bool fields. ?published=true used to bind the raw string
// "true", which SQLite (INTEGER column vs TEXT operand) never matches —
// only ?published=1 worked. Parsing must coerce ParseBool-shaped values
// on Bool fields so the binder receives a real boolean, which every
// dialect maps correctly (PG native bool, SQLite 1/0).
func TestBoolFilterValuesBindAsBools(t *testing.T) {
	fields := []schema.Field{
		{Name: "published", Type: schema.Bool},
		{Name: "title", Type: schema.String},
	}
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{"true", true},
		{"false", false},
		{"1", true},
		{"0", false},
	} {
		req := httptest.NewRequest(http.MethodGet, "/?published="+tc.raw, nil)
		filters, err := ParseFilters(req, fields)
		if err != nil {
			t.Fatalf("published=%q: parse error: %v", tc.raw, err)
		}

		qb := query.Select("*").From("things")
		ApplyToQuery(qb, filters)
		_, args := qb.Build()
		if len(args) != 1 {
			t.Fatalf("published=%q: got %d args (%v), want 1", tc.raw, len(args), args)
		}
		if got, ok := args[0].(bool); !ok || got != tc.want {
			t.Errorf("published=%q: list bound %T(%v), want bool(%v)", tc.raw, args[0], args[0], tc.want)
		}

		cb := query.Count("things")
		ApplyToCountQuery(cb, filters)
		_, cargs := cb.Build()
		if len(cargs) != 1 {
			t.Fatalf("published=%q: count got %d args (%v), want 1", tc.raw, len(cargs), cargs)
		}
		if got, ok := cargs[0].(bool); !ok || got != tc.want {
			t.Errorf("published=%q: count bound %T(%v), want bool(%v)", tc.raw, cargs[0], cargs[0], tc.want)
		}
	}
}

// TestBoolInFilterValuesBindAsBools extends the coercion to the _in
// expansion: each comma-separated part on a Bool field must bind as its
// own boolean, not as the raw string.
func TestBoolInFilterValuesBindAsBools(t *testing.T) {
	fields := []schema.Field{{Name: "published", Type: schema.Bool}}
	req := httptest.NewRequest(http.MethodGet, "/?published_in=true,false", nil)
	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Fatal(err)
	}
	qb := query.Select("*").From("things")
	ApplyToQuery(qb, filters)
	_, args := qb.Build()
	if len(args) != 2 {
		t.Fatalf("got %d args (%v), want 2", len(args), args)
	}
	got0, ok0 := args[0].(bool)
	got1, ok1 := args[1].(bool)
	if !ok0 || !got0 || !ok1 || got1 {
		t.Errorf("published_in=true,false bound %v (%T), want [true false]", args, args)
	}
}

// TestNonBoolFiltersStillBindStrings pins the unchanged paths: string
// fields bind raw strings, and a Bool field carrying a non-ParseBool
// value binds the raw string rather than erroring or guessing.
func TestNonBoolFiltersStillBindStrings(t *testing.T) {
	for _, tc := range []struct {
		q     string
		field string
	}{
		{"/?title=it's", "title"},
		{"/?published=maybe", "published"},
	} {
		fields := []schema.Field{
			{Name: "published", Type: schema.Bool},
			{Name: "title", Type: schema.String},
		}
		req := httptest.NewRequest(http.MethodGet, tc.q, nil)
		filters, err := ParseFilters(req, fields)
		if err != nil {
			t.Fatalf("%s: parse error: %v", tc.q, err)
		}
		qb := query.Select("*").From("things")
		ApplyToQuery(qb, filters)
		_, args := qb.Build()
		if len(args) != 1 {
			t.Fatalf("%s: got %d args (%v), want 1", tc.q, len(args), args)
		}
		if s, ok := args[0].(string); !ok {
			t.Errorf("%s: bound %T(%v), want the raw string", tc.q, args[0], args[0])
		} else if s == "" {
			t.Errorf("%s: bound empty string", tc.q)
		}
	}
}
