package filter

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// aliasedFields models a versioned entity: the column is author_id, the wire
// name clients are told to use is "writer".
func aliasedFields() []schema.Field {
	return []schema.Field{
		{Name: "id", Type: schema.Int},
		{Name: "author_id", Type: schema.String, WireName: "writer"},
		{Name: "secret_col", Type: schema.String, Hidden: true, WireName: "leaky"},
	}
}

// Every read path must accept the wire key a client is told to use. Resolving
// it on filters and projection but not on sort/where splits the wire contract
// by entry point, which is worse than either behaviour applied consistently.
func TestWireName_SortAcceptsAliasAndResolvesToColumn(t *testing.T) {
	sorts, err := ParseSortValues(url.Values{"sort": []string{"writer"}}, aliasedFields())
	if err != nil {
		t.Fatalf("?sort=writer rejected: %v", err)
	}
	if len(sorts) != 1 {
		t.Fatalf("got %d sort clauses, want 1", len(sorts))
	}
	// Field reaches ORDER BY — it must be the column, not the wire name.
	if sorts[0].Field != "author_id" {
		t.Errorf("sort field = %q, want the column %q", sorts[0].Field, "author_id")
	}
}

func TestWireName_SortStillAcceptsColumnName(t *testing.T) {
	sorts, err := ParseSortValues(url.Values{"sort": []string{"author_id"}}, aliasedFields())
	if err != nil {
		t.Fatalf("?sort=author_id rejected — existing callers would break: %v", err)
	}
	if len(sorts) != 1 || sorts[0].Field != "author_id" {
		t.Fatalf("got %+v, want one clause on author_id", sorts)
	}
}

func TestWireName_WhereAcceptsAliasAndResolvesToColumn(t *testing.T) {
	p, err := ParseWhere(`{"field":"writer","op":"eq","value":"alice"}`, aliasedFields())
	if err != nil {
		t.Fatalf("?where on wire name rejected: %v", err)
	}
	if p == nil {
		t.Fatal("nil predicate")
	}
	// Field reaches the WHERE clause — must be the real column.
	if p.Field != "author_id" {
		t.Errorf("predicate field = %q, want the column %q", p.Field, "author_id")
	}
	if sql := BuildPredicate(p).SQL; !strings.Contains(sql, "author_id") {
		t.Errorf("compiled SQL %q should reference the column", sql)
	}
}

// Nested groups resolve too — the alias map has to reach every leaf, not just
// a top-level one.
func TestWireName_WhereResolvesAliasInNestedGroup(t *testing.T) {
	p, err := ParseWhere(
		`{"or":[{"field":"writer","op":"eq","value":"a"},{"field":"id","op":"gt","value":"1"}]}`,
		aliasedFields())
	if err != nil {
		t.Fatalf("nested where rejected: %v", err)
	}
	sql := BuildPredicate(p).SQL
	if !strings.Contains(sql, "author_id") {
		t.Errorf("nested leaf did not resolve: %q", sql)
	}
	if strings.Contains(sql, "writer") {
		t.Errorf("wire name leaked into SQL: %q", sql)
	}
}

// A hidden field must stay unreachable under BOTH its column name and its wire
// alias. Registering the alias for a hidden field would reopen the
// value-disclosure oracle the Hidden skip exists to close.
func TestWireName_HiddenFieldUnreachableUnderAliasOnSortAndWhere(t *testing.T) {
	for _, name := range []string{"secret_col", "leaky"} {
		if _, err := ParseSortValues(url.Values{"sort": []string{name}}, aliasedFields()); err == nil {
			t.Errorf("?sort=%s on a hidden field was allowed", name)
		}
		if _, err := ParseWhere(`{"field":"`+name+`","op":"eq","value":"x"}`, aliasedFields()); err == nil {
			t.Errorf("?where on hidden field %q was allowed", name)
		}
	}
}
