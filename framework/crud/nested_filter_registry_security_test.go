package crud

import (
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// A relation may legitimately point at a real table that is not a registered
// entity, the auth battery self-migrates auth_users, and an app relates to it
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
	// A registry that knows posts but NOT auth_users, the shape a
	// self-migrating battery produces.
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts}}

	q := url.Values{"author.password_hash_like": {"$2a$"}}
	_, err := parseNestedFiltersValues(q, posts, reg)
	if err == nil {
		t.Fatal("SECURITY: a nested filter on an unregistered relation target was accepted; " +
			"the column reaches an EXISTS predicate with no schema check")
	}
	// The operator has to know WHICH target could not be resolved, or the
	// error sends them looking through every relation on the entity.
	if !strings.Contains(err.Error(), "auth_users") {
		t.Fatalf("error should name the unresolvable target, got: %v", err)
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

func TestBuildExistsSubquery_UnsafeRelationIdentifiers(t *testing.T) {
	cases := []struct {
		name string
		nf   nestedFilter
		ppk  string
	}{
		{
			name: "unsafe foreign key in BelongsTo",
			nf: nestedFilter{
				Relation: entity.Relation{Type: entity.RelManyToOne, Entity: "authors", ForeignKey: "author_id; DROP TABLE users"},
				Field:    "name",
				Op:       "eq",
				Value:    "alice",
			},
		},
		{
			name: "unsafe foreign key in HasMany",
			nf: nestedFilter{
				Relation: entity.Relation{Type: entity.RelHasMany, Entity: "comments", ForeignKey: "post_id; DROP TABLE users"},
				Field:    "content",
				Op:       "eq",
				Value:    "test",
			},
		},
		{
			name: "unsafe through in ManyToMany",
			nf: nestedFilter{
				Relation: entity.Relation{
					Type:             entity.RelManyToMany,
					Entity:           "tags",
					Through:          "post_tags; DROP TABLE users",
					LocalKey:         "post_id",
					ForeignKeyTarget: "tag_id",
				},
				Field: "name",
				Op:    "eq",
				Value: "go",
			},
		},
		{
			name: "unsafe local_key in ManyToMany",
			nf: nestedFilter{
				Relation: entity.Relation{
					Type:             entity.RelManyToMany,
					Entity:           "tags",
					Through:          "post_tags",
					LocalKey:         "post_id OR 1=1",
					ForeignKeyTarget: "tag_id",
				},
				Field: "name",
				Op:    "eq",
				Value: "go",
			},
		},
		{
			name: "unsafe target_key in ManyToMany",
			nf: nestedFilter{
				Relation: entity.Relation{
					Type:             entity.RelManyToMany,
					Entity:           "tags",
					Through:          "post_tags",
					LocalKey:         "post_id",
					ForeignKeyTarget: "tag_id OR 1=1",
				},
				Field: "name",
				Op:    "eq",
				Value: "go",
			},
		},
		{
			name: "unsafe parent pk",
			ppk:  "id; SELECT 1",
			nf: nestedFilter{
				Relation: entity.Relation{Type: entity.RelHasMany, Entity: "comments", ForeignKey: "post_id"},
				Field:    "content",
				Op:       "eq",
				Value:    "test",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ppk := tc.ppk
			if ppk == "" {
				ppk = "id"
			}
			sql, _ := buildExistsSubquery("posts", ppk, tc.nf)
			if strings.Contains(sql, ";") || strings.Contains(sql, "DROP") || strings.Contains(sql, "OR 1=1") {
				t.Fatalf("SECURITY: unsafe SQL generated for %s: %s", tc.name, sql)
			}
			if sql != "1 = 0" {
				t.Fatalf("expected '1 = 0' for unsafe identifier in %s, got %s", tc.name, sql)
			}
		})
	}
}
