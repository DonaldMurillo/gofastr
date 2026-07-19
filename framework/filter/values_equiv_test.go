package filter

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ParseFilters and ParseFiltersValues share their body via a thin wrapper.
// Pin the contract: for the same underlying query string they MUST produce
// the same ParsedFilter set (same fields, same ops, same values). Order is
// NOT part of the contract — the parse ranges over url.Values (a map), so
// output order is non-deterministic, and filters are AND-ed so order carries
// no meaning. Compared as a multiset. A refactor that fed one path a stale
// url.Values would still be caught (the set would differ).
func TestParseFilters_ValuesEquiv(t *testing.T) {
	fields := []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "score", Type: schema.Int},
		{Name: "tag", Type: schema.String},
	}
	cases := []string{
		"",
		"status=active",
		"status=active&score_gt=10&tag_in=a,b,c",
		"score_gte=5&score_lte=100",
		"unknownkey=value",
		"status=active&_internal=1",
		"q=hello",
	}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/things?"+raw, nil)
			viaReq, reqErr := ParseFilters(r, fields)
			viaValues, valuesErr := ParseFiltersValues(r.URL.Query(), fields)

			// Error equivalence: either both succeed or both fail with the
			// same message (the wrapper passes the same opts through).
			if (reqErr != nil) != (valuesErr != nil) {
				t.Fatalf("error divergence: req=%v values=%v", reqErr, valuesErr)
			}
			if reqErr != nil && reqErr.Error() != valuesErr.Error() {
				t.Fatalf("error message divergence: req=%q values=%q", reqErr, valuesErr)
			}
			if !filtersEqualAsSet(viaReq, viaValues) {
				t.Fatalf("filter divergence:\n  req:    %v\n  values: %v", viaReq, viaValues)
			}
		})
	}
}

// ParseSort and ParseSortValues share their body via a thin wrapper. Pin
// the same contract as above for the sort path.
func TestParseSort_ValuesEquiv(t *testing.T) {
	fields := []schema.Field{
		{Name: "status", Type: schema.String},
		{Name: "score", Type: schema.Int},
	}
	cases := []string{
		"",
		"sort=score",
		"sort=-status",
		"sort=score&sort=-status",
		"sort=unknown",
	}
	for _, raw := range cases {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/things?"+raw, nil)
			viaReq, reqErr := ParseSort(r, fields)
			viaValues, valuesErr := ParseSortValues(r.URL.Query(), fields)

			if (reqErr != nil) != (valuesErr != nil) {
				t.Fatalf("error divergence: req=%v values=%v", reqErr, valuesErr)
			}
			if reqErr != nil && reqErr.Error() != valuesErr.Error() {
				t.Fatalf("error message divergence: req=%q values=%q", reqErr, valuesErr)
			}
			if !sortsEqual(viaReq, viaValues) {
				t.Fatalf("sort divergence:\n  req:    %v\n  values: %v", viaReq, viaValues)
			}
		})
	}
}

// TestParseFiltersValues_ParsesExactlyOnce is a regression guard: it asserts
// the same url.Values, handed to two different helpers, yields the same
// field accessors that handing a freshly-parsed *http.Request would. This is
// what the CRUD List handler now relies on — passing ONE url.Values through
// every helper instead of N re-parses.
func TestParseFiltersValues_ParsesExactlyOnce(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}
	raw := "status=active&status=pending"
	q, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	a, errA := ParseFiltersValues(q, fields)
	b, errB := ParseFiltersValues(q, fields)
	if errA != nil || errB != nil {
		t.Fatalf("unexpected errors: %v %v", errA, errB)
	}
	// url.Values is a map — iterating it twice yields the same data, but the
	// ParsedFilter ORDER is non-deterministic. Compare as sets.
	if !filtersEqualAsSet(a, b) {
		t.Fatalf("expected idempotent re-parse:\n  a=%v\n  b=%v", a, b)
	}
}

func sortsEqual(a, b []ParsedSort) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func filtersEqualAsSet(a, b []ParsedFilter) bool {
	if len(a) != len(b) {
		return false
	}
	counts := make(map[ParsedFilter]int, len(a))
	for _, f := range a {
		counts[f]++
	}
	for _, f := range b {
		counts[f]--
		if counts[f] < 0 {
			return false
		}
	}
	return true
}
