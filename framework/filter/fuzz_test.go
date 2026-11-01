package filter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// FuzzParseFilters drives the REST filter parser with an arbitrary raw
// query string against a fixed field set. Contract is liveness: parsing
// attacker-controlled query params must always terminate and never panic
// (malformed operators, repeated keys, control bytes, oversized IN lists).
func FuzzParseFilters(f *testing.F) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "status", Type: schema.String},
		{Name: "count", Type: schema.Int},
		{Name: "secret", Type: schema.String, Hidden: true},
		// title_in collides with title's _in: the bare key reads two
		// ways and only the bracket spellings reach either field.
		{Name: "title_in", Type: schema.String},
	}

	for _, s := range []string{
		"", "title=foo", "title_like=Fir%", "count_gte=3&count_lte=9",
		"status_in=a,b,c", "title=foo'--", "secret=x",
		"sort=title&sort=-status", "title=\x00\n", "bad%ZZ=1",
		"count_gte=notanumber", "title_in=" + "a," + "b,",
		"title[eq]=x", "title[in]=a,b&title[in]=c", "title_in[eq]=x",
		"count[gte]=3&count_gte=4", "title[foo]=x", "title%5Blike%5D=a",
		"secret[eq]=x", "[eq]=x", "title[=x", "title]=x", "title[eq][in]=x",
		"title[]=", "count[lte]=1&count[lte]=2",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, rawQuery string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.RawQuery = rawQuery
		// Contract: returns, never panics, and no spelling (suffix,
		// bracket, percent-encoded) builds a predicate on the Hidden
		// column.
		fs, _ := ParseFilters(req, fields)
		for _, pf := range fs {
			if pf.Field == "secret" {
				t.Fatalf("?%s built a filter on the Hidden field: %+v", rawQuery, pf)
			}
		}
	})
}

// FuzzParseWhere: ParseWhere returns, never panics, on any ?where= text,
// and a tree it accepts compiles.
func FuzzParseWhere(f *testing.F) {
	fields := []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "count", Type: schema.Int},
	}
	for _, s := range []string{
		`{"field":"status","value":"a"}`,
		`{"or":[{"field":"count","op":"gte","value":"3"},{"and":[{"field":"status","op":"in","values":["a","b"]}]}]}`,
		`{"field":"status","value":"a","VALUE":"b"}`,
		`{"and":[],"field":"status"}`,
		`{"field":null,"op":null,"values":null}`,
		`{"or":null}`,
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		p, err := ParseWhere(raw, fields)
		if err == nil && p != nil {
			_ = BuildPredicate(p)
		}
	})
}
