package filter

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestFilter_HiddenFieldNoPredicate verifies a Hidden field can never be
// used as a WHERE predicate over the HTTP List surface. Otherwise an
// attacker probes prefixes via ?password_hash_like=... and observes
// row-count/result changes to exfiltrate a Hidden column — a value
// oracle. Mirrors ParseSort's existing Hidden exclusion.
func TestFilter_HiddenFieldNoPredicate(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String},
		{Name: "password_hash", Type: schema.String, Hidden: true},
	}

	hiddenUsed := func(filters []ParsedFilter) bool {
		for _, f := range filters {
			if f.Field == "password_hash" {
				return true
			}
		}
		return false
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
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if hiddenUsed(filters) {
				t.Errorf("SECURITY: Hidden field password_hash became a filter predicate via %q — value-disclosure oracle", tc.query)
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
