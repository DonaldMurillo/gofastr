package crud

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A relation may legitimately point at a real table that is not a registered
// entity — the auth battery self-migrates auth_users, and an app relates to it
// by name. parseIncludeTree refuses that shape because every eager-load guard
// hangs off the target's schema. Nested filters used to trust it instead: the
// Hidden/NoQuery/declared checks sat inside `if registry.Get(...) == nil`, so
// an unresolvable target skipped all three and buildExistsSubquery
// interpolated the caller's column name into
//
//	EXISTS (SELECT 1 FROM auth_users WHERE ... AND auth_users.password_hash LIKE $1)
//
// isSafeIdentifier only enforces that the name LOOKS like an identifier, so
// ?author.password_hash_like=$2a$ came back 200 with a row set that varies
// with the stored value: character-by-character recovery of a column that is
// not in any response.
func TestNestedFilterRefusesUnregisteredTarget(t *testing.T) {
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "auth_users", "author_id"),
		},
	})
	// A registry that knows posts but NOT auth_users — the shape a
	// self-migrating battery produces.
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts}}

	q := url.Values{"author.password_hash_like": {"$2a$"}}
	_, err := parseNestedFiltersValues(q, posts, reg)
	if err == nil {
		t.Fatal("SECURITY: a nested filter on an unregistered relation target was accepted; " +
			"the column reaches an EXISTS predicate with no schema check")
	}
	if !strings.Contains(err.Error(), "not registered") {
		t.Fatalf("error should name the missing registration, got: %v", err)
	}
}

// The in-process twin has to refuse identically or a typed repository is the
// way around the HTTP guard.
func TestResolveNestedFiltersRefusesUnregisteredTarget(t *testing.T) {
	posts := entity.Define("posts", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "author_id", Type: schema.String},
		},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "auth_users", "author_id"),
		},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts}}

	_, err := resolveNestedFilters(posts, reg, []NestedFilter{
		{Relation: "author", Field: "password_hash", Value: "x"},
	})
	if err == nil {
		t.Fatal("SECURITY: in-process nested filter accepted an unregistered relation target")
	}
}

// No registry at all is the wider version of the same hole: nothing can be
// checked, so nothing may be filtered. parseIncludeTreeQ already answers this
// way for ?include=.
func TestNestedFilterRefusesWithoutRegistry(t *testing.T) {
	posts := entity.Define("posts", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "auth_users", "author_id")},
	})

	q := url.Values{"author.password_hash_like": {"$2a$"}}
	if _, err := parseNestedFiltersValues(q, posts, nil); err == nil {
		t.Fatal("SECURITY: a nested filter was applied with no registry to validate it against")
	}
}
