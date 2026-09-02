package crud

import (
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// The IN entry cap (filter.MaxINListEntries) must hold on EVERY surface
// that accepts an IN list. The surfaces:
//
//   - flat ?field_in= (filter.ParseFiltersValues)
//   - ?where= JSON "values" (filter.ParseWhere)
//   - nested ?rel.field_in= (parseNestedFiltersValues)
//   - the in-process NestedFilter.Values slice (resolveNestedFilters)
//
// The first three enforce the cap (the first two are asserted here as
// the reference behaviour; the nested HTTP one documents that it shares
// "the entry cap the flat path enforces, same cap, same error shape").
// The in-process surface did not: resolveNestedFilters is documented as
// "running the same relation/field validation and identifier-safety
// checks the HTTP path applies", but it never bounds Values, and
// buildExistsSubquery emits one placeholder per value — the same
// statement-size / placeholder-count vector the cap exists to bound,
// reachable by a typed repository threading a request-derived slice.

// inCapFixture builds a posts-handler whose posts have many comments,
// with a registry resolving the relation, mirroring the harness in
// nested_filter_owner_scope_test.go.
func inCapFixture(t *testing.T) (*CrudHandler, *testRegistry) {
	t.Helper()
	ddl := `
CREATE TABLE posts (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	title     TEXT
);
CREATE TABLE comments (
	id        TEXT PRIMARY KEY,
	user_id   TEXT NOT NULL,
	post_id   TEXT NOT NULL,
	body      TEXT
);
`
	postCfg := makeEntityConfig("posts", "posts", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "title", Type: schema.String},
		},
		func(c *entity.EntityConfig) {
			c.Relations = []entity.Relation{entity.HasMany("comments", "comments", "post_id")}
		},
	)
	commentCfg := makeEntityConfig("comments", "comments", "user_id",
		[]schema.Field{
			{Name: "user_id", Type: schema.String, Required: true},
			{Name: "post_id", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String},
		},
	)

	ch, db := setupSecurityTestHandler(t, postCfg, ddl)
	commentEnt := entity.Define(commentCfg.Table, commentCfg)
	commentEnt.SetDB(db)
	reg := newTestRegistry(t)
	reg.add(t, ch.Entity)
	reg.add(t, commentEnt)
	ch.Registry = reg

	seedRows(t, db, "posts", []map[string]any{
		{"id": "p1", "user_id": "alice", "title": "alice post"},
	})
	seedRows(t, db, "comments", []map[string]any{
		{"id": "c-1", "user_id": "alice", "post_id": "p1", "body": "first"},
		{"id": "c-2", "user_id": "bob", "post_id": "p1", "body": "second"},
	})
	return ch, reg
}

// TestNestedINListCapEverySurface: one over-cap list (cap+1 entries)
// is refused on every IN surface, and an at-cap list still resolves,
// so the refusal is the cap and not a blanket rejection.
func TestNestedINListCapEverySurface(t *testing.T) {
	ch, reg := inCapFixture(t)
	fields := []schema.Field{{Name: "title", Type: schema.String}}

	overCapCommas := func(n int) string { return strings.Repeat("v,", n-1) + "v" }

	t.Run("flat_field_in_reference", func(t *testing.T) {
		q := url.Values{"title_in": {overCapCommas(filter.MaxINListEntries + 1)}}
		if _, err := filter.ParseFiltersValues(q, fields); err == nil {
			t.Error("flat ?title_in= accepted an over-cap list")
		}
	})
	t.Run("where_json_values_reference", func(t *testing.T) {
		vals := make([]string, filter.MaxINListEntries+1)
		for i := range vals {
			vals[i] = `"v"`
		}
		raw := fmt.Sprintf(`{"field":"title","op":"in","values":[%s]}`, strings.Join(vals, ","))
		if _, err := filter.ParseWhere(raw, fields); err == nil {
			t.Error("?where= accepted an over-cap values array")
		}
	})

	t.Run("where_json_comma_value_reference", func(t *testing.T) {
		raw := fmt.Sprintf(`{"field":"title","op":"in","value":"%s"}`, overCapCommas(filter.MaxINListEntries+1))
		if _, err := filter.ParseWhere(raw, fields); err == nil {
			t.Error("?where= accepted an over-cap comma-separated value string")
		}
	})
	t.Run("nested_http_field_in", func(t *testing.T) {
		q := url.Values{"comments.body_in": {overCapCommas(filter.MaxINListEntries + 1)}}
		if _, err := parseNestedFiltersValues(q, ch.Entity, reg); err == nil {
			t.Error("nested ?comments.body_in= accepted an over-cap list")
		}
	})
	t.Run("in_process_values", func(t *testing.T) {
		vals := make([]string, filter.MaxINListEntries+1)
		for i := range vals {
			vals[i] = "v"
		}
		_, err := resolveNestedFilters(ch.Entity, reg, []NestedFilter{
			{Relation: "comments", Field: "body", Op: filter.OpIn, Values: vals},
		})
		if err == nil {
			t.Error("in-process NestedFilter accepted an over-cap Values slice; the cap holds on " +
				"every other IN surface and this one emits one placeholder per value into the EXISTS clause")
		}
	})

	// Control: an at-cap list resolves on the in-process surface too.
	atCap := make([]string, filter.MaxINListEntries)
	for i := range atCap {
		atCap[i] = "v"
	}
	if _, err := resolveNestedFilters(ch.Entity, reg, []NestedFilter{
		{Relation: "comments", Field: "body", Op: filter.OpIn, Values: atCap},
	}); err != nil {
		t.Errorf("at-cap (%d entries) in-process IN list rejected: %v", filter.MaxINListEntries, err)
	}
}
