package filter

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

func noQueryFields() []schema.Field {
	return []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "card", Type: schema.String, NoQuery: true},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	}
}

// TestNoQueryFieldNoPredicate pins that a NoQuery column can never become a
// WHERE predicate. A field masked on the way out but left filterable is
// recoverable a character at a time from row presence — the same oracle the
// Hidden exclusion blocks, against a field that must stay in the response.
func TestNoQueryFieldNoPredicate(t *testing.T) {
	queries := []string{
		"card=4111",
		"card_like=4111",
		"card_gt=4000",
		"card_gte=4000",
		"card_lt=5000",
		"card_lte=5000",
		"card_in=4111,4112",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+q, nil)
			filters, err := ParseFilters(req, noQueryFields())
			if err == nil {
				t.Fatalf("SECURITY: %q must fail closed, got filters=%+v", q, filters)
			}
			if len(filters) != 0 {
				t.Errorf("SECURITY: %q produced %d predicate(s); none may reach the WHERE clause", q, len(filters))
			}
		})
	}
}

// TestNoQuerySortRejected covers the ordering oracle: ORDER BY on a masked
// column leaks the relative values of every row.
func TestNoQuerySortRejected(t *testing.T) {
	for _, q := range []string{"sort=card", "sort=-card"} {
		t.Run(q, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+q, nil)
			sorts, err := ParseSort(req, noQueryFields())
			if err == nil {
				t.Fatalf("SECURITY: %q must fail closed, got sorts=%+v", q, sorts)
			}
			if len(sorts) != 0 {
				t.Errorf("SECURITY: %q produced %d sort clause(s)", q, len(sorts))
			}
		})
	}
}

// TestNoQueryWhereRejected covers the ?where= predicate-tree surface, including
// a NoQuery leaf buried inside an and/or node.
func TestNoQueryWhereRejected(t *testing.T) {
	trees := []string{
		`{"field":"card","op":"like","value":"4111"}`,
		`{"and":[{"field":"title","value":"x"},{"field":"card","value":"4111"}]}`,
	}
	for _, raw := range trees {
		p, err := ParseWhere(raw, noQueryFields())
		if err == nil {
			t.Fatalf("SECURITY: where %s must fail closed, got %+v", raw, p)
		}
		if !strings.Contains(err.Error(), "card") {
			t.Errorf("where error %q should name the field", err)
		}
	}
}

// TestNoQueryErrorNamesFieldHiddenDoesNot pins the deliberate asymmetry. A
// Hidden field must be indistinguishable from an absent one — its existence is
// the secret. A NoQuery field is in the response, so naming it in the error
// discloses nothing and tells the developer what actually went wrong.
func TestNoQueryErrorNamesFieldHiddenDoesNot(t *testing.T) {
	fields := noQueryFields()

	_, noQueryErr := ParseFilters(httptest.NewRequest(http.MethodGet, "/?card=4111", nil), fields)
	if noQueryErr == nil {
		t.Fatal("NoQuery filter must error")
	}
	if !strings.Contains(noQueryErr.Error(), "card") || !strings.Contains(noQueryErr.Error(), "cannot be") {
		t.Errorf("NoQuery error = %q, want it to name \"card\" and say it cannot be filtered", noQueryErr)
	}

	_, hiddenErr := ParseFilters(httptest.NewRequest(http.MethodGet, "/?password_hash=x", nil), fields)
	if hiddenErr == nil {
		t.Fatal("Hidden filter must error")
	}
	_, absentErr := ParseFilters(httptest.NewRequest(http.MethodGet, "/?nosuchfield=x", nil), fields)
	if absentErr == nil {
		t.Fatal("absent filter must error")
	}
	hiddenShape := strings.ReplaceAll(hiddenErr.Error(), "password_hash", "FIELD")
	absentShape := strings.ReplaceAll(absentErr.Error(), "nosuchfield", "FIELD")
	if hiddenShape != absentShape {
		t.Errorf("SECURITY: Hidden and absent errors differ beyond the field name (%q vs %q); "+
			"the difference tells an attacker the column exists", hiddenErr, absentErr)
	}
}

// TestNoQueryLenientDropsSilently pins that Lenient mode keeps its
// drop-don't-reject contract for NoQuery keys — and still builds no predicate.
func TestNoQueryLenientDropsSilently(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/?card_like=4111&title=x", nil)
	filters, err := ParseFilters(req, noQueryFields(), Lenient())
	if err != nil {
		t.Fatalf("lenient mode must not error: %v", err)
	}
	for _, f := range filters {
		if f.Field == "card" {
			t.Fatalf("SECURITY: lenient mode built a predicate on a NoQuery field: %+v", f)
		}
	}
	if len(filters) != 1 || filters[0].Field != "title" {
		t.Errorf("filters = %+v, want only the title predicate", filters)
	}
}

// A WireName is the key clients are told to send, and the filter parser
// resolves it to the column before building the WHERE clause. That resolution
// runs BEFORE the NoQuery refusal is consulted, so registering the alias in
// the allow-set — as the plain path does — would make the wire key a way
// straight around the guard. Neither the aliasing work nor the NoQuery work
// has this hole alone; only both in one tree.
func TestNoQueryRefusedUnderItsWireName(t *testing.T) {
	fields := []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "author_id", Type: schema.String, WireName: "writer"},
		{Name: "card_number", Type: schema.String, WireName: "pan", NoQuery: true},
	}

	// Control: an ordinary field's alias resolves to its column.
	got, err := ParseFiltersValues(url.Values{"writer": {"u1"}}, fields)
	if err != nil {
		t.Fatalf("an ordinary wire alias must filter: %v", err)
	}
	if len(got) != 1 || got[0].Field != "author_id" {
		t.Fatalf("wire alias did not resolve to the column: %#v", got)
	}

	// The masked column is refused under the column name AND the wire name,
	// with and without an operator suffix.
	for _, key := range []string{"card_number", "pan", "card_number_like", "pan_like", "pan_in"} {
		if _, err := ParseFiltersValues(url.Values{key: {"4111"}}, fields); err == nil {
			t.Errorf("SECURITY: ?%s= was accepted on a NoQuery column", key)
		}
	}
	// And on the sort surface.
	for _, s := range []string{"pan", "-pan", "card_number"} {
		if _, err := ParseSortValues(url.Values{"sort": {s}}, fields); err == nil {
			t.Errorf("SECURITY: ?sort=%s was accepted on a NoQuery column", s)
		}
	}
	if _, err := ParseSortValues(url.Values{"sort": {"writer"}}, fields); err != nil {
		t.Errorf("an ordinary wire alias must still sort: %v", err)
	}
}
