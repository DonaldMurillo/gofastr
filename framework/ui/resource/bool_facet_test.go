package resource

import (
	"net/url"
	"testing"
)

// The bool facet renders Yes/No options with values "true"/"false"
// (blueprint-generated apps ship this UI on SQLite by default), so the
// filter it builds must bind a real bool — a raw "true" string binds TEXT
// against SQLite's INTEGER storage and the facet silently shows zero rows.
func TestBoolFacetFilterBindsBool(t *testing.T) {
	c := Config{Filters: []Filter{{Key: "published", Label: "Published", Type: "bool"}}}
	q := url.Values{"published": {"true"}}
	fs := c.queryFilters(q, "")
	if len(fs) != 1 {
		t.Fatalf("filters = %v, want 1", fs)
	}
	if got := fs[0].BindValue(); got != true {
		t.Fatalf("BindValue() = %#v, want bool true", got)
	}
}

func TestEnumFacetFilterStaysString(t *testing.T) {
	c := Config{Filters: []Filter{{Key: "status", Label: "Status", Type: "enum", Values: []string{"true", "draft"}}}}
	q := url.Values{"status": {"true"}}
	fs := c.queryFilters(q, "")
	if len(fs) != 1 {
		t.Fatalf("filters = %v, want 1", fs)
	}
	if got := fs[0].BindValue(); got != "true" {
		t.Fatalf("BindValue() = %#v, want the string \"true\"", got)
	}
}
