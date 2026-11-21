package filter

import (
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ParseFilters and ParseFiltersValues share their body via a thin wrapper.
// Pin the contract: for the same underlying query string they MUST produce
// the same ParsedFilter set (same fields, same ops, same values). Order is
// NOT part of the contract, the parse ranges over url.Values (a map), so
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
		t.Run(raw, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/things?"+raw, nil)
			viaReq, reqErr := ParseFilters(r, fields)
			viaValues, valuesErr := ParseFiltersValues(r.URL.Query(), fields)

			requireSameOutcome(t, reqErr, valuesErr)
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
		"sort=score,-status",
		"sort=unknown",
	}
	for _, raw := range cases {
		t.Run(raw, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/things?"+raw, nil)
			viaReq, reqErr := ParseSort(r, fields)
			viaValues, valuesErr := ParseSortValues(r.URL.Query(), fields)

			requireSameOutcome(t, reqErr, valuesErr)
			if !sortsEqual(viaReq, viaValues) {
				t.Fatalf("sort divergence:\n  req:    %v\n  values: %v", viaReq, viaValues)
			}
		})
	}
}

// requireSameOutcome pins the wrapper contract both equivalence tests share:
// either both paths succeed or both fail with the same message (the wrapper
// passes the same opts through).
func requireSameOutcome(t *testing.T, reqErr, valuesErr error) {
	t.Helper()
	if (reqErr != nil) != (valuesErr != nil) {
		t.Fatalf("error divergence: req=%v values=%v", reqErr, valuesErr)
	}
	if reqErr != nil && reqErr.Error() != valuesErr.Error() {
		t.Fatalf("error message divergence: req=%q values=%q", reqErr, valuesErr)
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
