package filter

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
)

// TestIN_MultiValueMatchesSet verifies that ?status_in=active,pending
// renders a single satisfiable IN/OR predicate over the value set rather
// than ANDing one equality per value (status=$1 AND status=$2), which is
// never true for a single row and silently returns zero results.
func TestIN_MultiValueMatchesSet(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}

	req := httptest.NewRequest(http.MethodGet, "/?status_in=active,pending", nil)
	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Build a query and assert the predicate is a real set membership.
	qb := query.Select("*").From("notes")
	ApplyToQuery(qb, filters)
	sql, args := qb.Build()

	// Both values must be bound.
	if len(args) != 2 {
		t.Fatalf("expected 2 bound args (active, pending), got %d: %v", len(args), args)
	}
	gotVals := map[string]bool{}
	for _, a := range args {
		if s, ok := a.(string); ok {
			gotVals[s] = true
		}
	}
	if !gotVals["active"] || !gotVals["pending"] {
		t.Fatalf("expected args to include active and pending, got %v", args)
	}

	// The predicate must be satisfiable for a single row: it must NOT be
	// "status = $1 AND status = $2" (which no row can satisfy). Accept
	// either an IN (...) form or an OR of equalities.
	hasIN := strings.Contains(sql, "IN (")
	hasOR := strings.Contains(sql, "OR")
	andEq := strings.Count(sql, "status") == 2 && strings.Contains(sql, "AND") && !hasIN && !hasOR
	if andEq {
		t.Errorf("multi-value IN rendered as unsatisfiable AND-of-equalities: %q. Want IN (...) or OR.", sql)
	}
	if !hasIN && !hasOR {
		t.Errorf("multi-value IN did not render a set predicate: %q", sql)
	}
}

// TestIN_MultiValueCountQuery mirrors TestIN_MultiValueMatchesSet for the
// count builder, which feeds pagination totals.
func TestIN_MultiValueCountQuery(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}

	req := httptest.NewRequest(http.MethodGet, "/?status_in=active,pending,archived", nil)
	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cb := query.Count("notes")
	ApplyToCountQuery(cb, filters)
	sql, args := cb.Build()

	if len(args) != 3 {
		t.Fatalf("expected 3 bound args, got %d: %v", len(args), args)
	}
	if !strings.Contains(sql, "IN (") && !strings.Contains(sql, "OR") {
		t.Errorf("count multi-value IN did not render a set predicate: %q", sql)
	}
	if strings.Contains(sql, "status = ") && strings.Contains(sql, "AND") && !strings.Contains(sql, "IN (") && !strings.Contains(sql, "OR") {
		t.Errorf("count multi-value IN rendered as unsatisfiable AND-of-equalities: %q", sql)
	}
}

// TestIN_SingleValueUnchanged verifies the single-value _in path still
// produces a working equality/membership predicate with one bound arg.
func TestIN_SingleValueUnchanged(t *testing.T) {
	fields := []schema.Field{{Name: "status", Type: schema.String}}

	req := httptest.NewRequest(http.MethodGet, "/?status_in=active", nil)
	filters, err := ParseFilters(req, fields)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	qb := query.Select("*").From("notes")
	ApplyToQuery(qb, filters)
	sql, args := qb.Build()

	if len(args) != 1 {
		t.Fatalf("expected 1 bound arg, got %d: %v", len(args), args)
	}
	if got, _ := args[0].(string); got != "active" {
		t.Fatalf("expected arg active, got %v", args[0])
	}
	if !strings.Contains(sql, "status") {
		t.Fatalf("expected status predicate, got %q", sql)
	}
}
