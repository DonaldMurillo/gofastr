package filter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// replaceField swaps a field name inside a raw query string so the "absent"
// probe mirrors the hidden probe byte-for-byte apart from the field name.
func replaceField(query, from, to string) string {
	return strings.Replace(query, from, to, 1)
}

// sameErrorShape reports whether two error strings are identical once the
// two field names are normalized away — i.e. they differ ONLY by the field
// name substituted in. Divergence beyond the name would leak hidden-vs-absent.
func sameErrorShape(hiddenMsg, absentMsg, hiddenName, absentName string) bool {
	norm := func(s, name string) string { return strings.ReplaceAll(s, name, "FIELD") }
	return norm(hiddenMsg, hiddenName) == norm(absentMsg, absentName)
}

// TestFilter_HiddenFieldNoPredicate verifies a Hidden field can never be
// used as a WHERE predicate over the HTTP List surface. Otherwise an
// attacker probes prefixes via ?password_hash_like=... and observes
// row-count/result changes to exfiltrate a Hidden column — a value
// oracle. Mirrors ParseSort's existing Hidden exclusion.
//
// Under strict parsing a hidden-field key is REJECTED (not silently
// dropped), which strengthens the invariant — no predicate is built AND
// the request fails closed. The rejection must be NON-LEAKY: a hidden field
// and a truly-nonexistent field must produce the identical error shape, so
// the error cannot be used to distinguish "hidden" from "absent".
func TestFilter_HiddenFieldNoPredicate(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	}

	cases := []struct {
		name  string
		query string
	}{
		{"hidden_eq", "password_hash=$2a$10$abc"},
		{"hidden_like", "password_hash_like=$2a$10$"},
		{"hidden_gte", "password_hash_gte=a"},
		{"hidden_in", "password_hash_in=a,b,c"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/?"+tc.query, nil)
			filters, err := ParseFilters(req, fields)
			if err == nil {
				t.Fatalf("SECURITY: Hidden field filter %q must fail closed, got no error (filters=%+v)", tc.query, filters)
			}
			if len(filters) != 0 {
				t.Errorf("SECURITY: Hidden field password_hash produced a predicate via %q — value-disclosure oracle", tc.query)
			}
			// Non-leaky: a nonexistent field of the same shape must give the
			// same "unknown filter" wording. If the messages diverged, the
			// error would itself be a hidden-column oracle.
			absent := httptest.NewRequest(http.MethodGet, "/?"+replaceField(tc.query, "password_hash", "nonexistent"), nil)
			_, absentErr := ParseFilters(absent, fields)
			if absentErr == nil {
				t.Fatalf("nonexistent field must also error")
			}
			if !sameErrorShape(err.Error(), absentErr.Error(), "password_hash", "nonexistent") {
				t.Errorf("SECURITY: hidden vs absent errors differ — oracle:\n hidden: %v\n absent: %v", err, absentErr)
			}
		})
	}

	// Happy path: a normal (non-hidden) field is still filterable.
	t.Run("visible_field_ok", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/?title=hello", nil)
		filters, err := ParseFilters(req, fields)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(filters) != 1 || filters[0].Field != "title" {
			t.Fatalf("visible field filter dropped: %+v", filters)
		}
	})
}
