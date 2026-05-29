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
	}

	for _, s := range []string{
		"", "title=foo", "title_like=Fir%", "count_gte=3&count_lte=9",
		"status_in=a,b,c", "title=foo'--", "secret=x",
		"sort=title&sort=-status", "title=\x00\n", "bad%ZZ=1",
		"count_gte=notanumber", "title_in=" + "a," + "b,",
	} {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, rawQuery string) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.URL.RawQuery = rawQuery
		// Contract: returns, never panics. Result intentionally ignored.
		_, _ = ParseFilters(req, fields)
	})
}
