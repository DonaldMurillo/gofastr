package crud

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// Over HTTP a nested filter whose hop reaches a target the caller may not
// read is refused, on the parsed (multi-hop) shape as on the one-hop one.
func TestNestedFilterHopRefusesUnreadableTarget(t *testing.T) {
	gated := gateEntity(t, "secrets", entity.EntityConfig{
		Exposure: &entity.ExposureConfig{CRUD: new(true), Access: entity.AccessControl{Read: "secrets:read"}},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"secrets": gated}}
	parent := gateEntity(t, "boards", entity.EntityConfig{Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true}})
	ch := &CrudHandler{Entity: parent, Registry: &reg}
	rel := entity.Relation{Type: entity.RelHasMany, Name: "secrets", Entity: "secrets", ForeignKey: []string{"board_id"}}
	nf := []nestedFilter{{Hops: []relationHop{{Relation: rel, Target: gated, Table: "secrets"}}, Relation: rel, Field: "body"}}
	if err := ch.scopeNestedFiltersForCaller(context.Background(), nf); err == nil {
		t.Fatal("a hop onto a target behind a read permission was permitted")
	}
}

// A nested filter value that is not of the target column's type is a
// parse error, never a bind the database judges.
func TestNestedFilterValueTypeChecked(t *testing.T) {
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users", Fields: []schema.Field{{Name: "age", Type: schema.Int}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:    []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"users": users, "posts": posts}}
	if _, err := parseNestedFiltersValues(url.Values{"author.age": {"abc"}}, posts, reg); err == nil {
		t.Fatal("?author.age=abc parsed")
	}
	if _, err := parseNestedFiltersValues(url.Values{"author.age": {"5"}}, posts, reg); err != nil {
		t.Fatalf("control: ?author.age=5 refused: %v", err)
	}
}

// A many_to_many hop's pivot scopes reach the SQL: a link row its own
// route hides links nothing.
func TestNestedFilterPivotScopesReachSQL(t *testing.T) {
	rel := entity.ManyToMany("tags", "tags", "post_tags", "post_id", "tag_id")
	tags := entity.Define("tags", entity.EntityConfig{
		Name: "tags", Table: "tags", Fields: []schema.Field{{Name: "label", Type: schema.String}},
	}.WithTimestamps(false))
	nf := nestedFilter{
		Hops: []relationHop{{
			Relation: rel, Target: tags, Table: "tags",
			PivotScopes: []filter.ParsedFilter{{Field: "user_id", Op: filter.OpEq, Value: "u1"}},
		}},
		Relation: rel, Field: "label", Op: filter.OpEq, Value: "go",
	}
	sql, args := buildExistsSubquery("posts", []string{"id"}, nf)
	if !strings.Contains(sql, "post_tags.user_id") || len(args) != 2 {
		t.Fatalf("pivot scope missing: %s %v", sql, args)
	}
}

// A nested field name is one plain identifier: a dotted, spaced or
// punctuated name never reaches the SQL.
func TestNestedFilterFieldIsOneIdentifier(t *testing.T) {
	for _, bad := range []string{"a.b", "a b", "a;--", "name OR 1=1"} {
		if isSafeIdentifier(bad) {
			t.Errorf("isSafeIdentifier(%q) = true", bad)
		}
	}
	rel := entity.Relation{Type: entity.RelHasMany, Name: "c", Entity: "c", ForeignKey: []string{"p_id"}}
	sql, _ := buildExistsSubquery("p", []string{"id"}, nestedFilter{Relation: rel, Field: "x OR 1=1", Op: filter.OpEq, Value: "v"})
	if sql != "1 = 0" {
		t.Fatalf("unsafe field rendered %q, want 1 = 0", sql)
	}
}

// In process, a nested filter carries the target's own BeforeList scope
// only under WithReadHooks, the opt-in every in-process read honours.
func TestNestedFilterInProcessHookScopesAreOptIn(t *testing.T) {
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users", Fields: []schema.Field{{Name: "age", Type: schema.Int}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:    []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"users": users, "posts": posts}}
	userHooks := hook.NewHookRegistry()
	userHooks.RegisterHook(hook.BeforeList, func(_ context.Context, data any) error {
		data.(*hook.ListPayload).AddWhere("age > $1", 0)
		return nil
	})
	ch := &CrudHandler{Entity: posts, Registry: reg, ChildHooks: func(name string) *hook.HookRegistry {
		if name == "users" {
			return userHooks
		}
		return nil
	}}
	scoped := func(ctx context.Context) int {
		nf, err := resolveNestedFilters(posts, reg, []NestedFilter{{Relation: "author", Field: "age", Op: filter.OpEq, Value: "5"}})
		if err != nil {
			t.Fatal(err)
		}
		if err := ch.scopeNestedFiltersInProcess(ctx, nf); err != nil {
			t.Fatal(err)
		}
		return len(nf[0].Hops[0].HookScopes)
	}
	if n := scoped(context.Background()); n != 0 {
		t.Errorf("without WithReadHooks the target's BeforeList ran (%d clauses)", n)
	}
	if n := scoped(WithReadHooks(context.Background())); n != 1 {
		t.Errorf("under WithReadHooks the target's BeforeList gave %d clauses, want 1", n)
	}
}

// An in-process nested filter naming its field by wire name reaches SQL
// as the column, as the HTTP form does.
func TestInProcessNestedFilterEmitsColumn(t *testing.T) {
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users", Fields: []schema.Field{{Name: "display_name", WireName: "displayName", Type: schema.String}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:    []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"users": users, "posts": posts}}
	nf, err := resolveNestedFilters(posts, reg, []NestedFilter{{Relation: "author", Field: "displayName", Op: filter.OpEq, Value: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if nf[0].Field != "display_name" {
		t.Fatalf("field = %q, want the column display_name", nf[0].Field)
	}
}

// A hop whose foreign key does not line up with the key it references
// renders as 1 = 0, even when no parser refused it first.
func TestNestedFilterMisalignedHopMatchesNothing(t *testing.T) {
	child := entity.Define("lines", entity.EntityConfig{Name: "lines", Table: "lines",
		Fields: []schema.Field{{Name: "a", Type: schema.String}, {Name: "b", Type: schema.String}}}.WithTimestamps(false))
	rel := entity.Relation{Type: entity.RelHasMany, Name: "lines", Entity: "lines", ForeignKey: []string{"a", "b"}}
	nf := nestedFilter{Hops: []relationHop{{Relation: rel, Target: child, Table: "lines"}}, Relation: rel, Field: "a", Op: filter.OpEq, Value: "x"}
	if sql, _ := buildExistsSubquery("orders", []string{"id"}, nf); sql != "1 = 0" {
		t.Fatalf("misaligned hop rendered %q, want 1 = 0", sql)
	}
}

// One query string always yields the same nested-filter order, so the
// same SQL text and the same first refusal.
func TestNestedFilterOrderIsStable(t *testing.T) {
	users := entity.Define("users", entity.EntityConfig{
		Name: "users", Table: "users", Fields: []schema.Field{{Name: "a", Type: schema.String}, {Name: "b", Type: schema.String}, {Name: "c", Type: schema.String}},
	}.WithTimestamps(false))
	posts := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields:    []schema.Field{{Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "users", "author_id")},
	}.WithTimestamps(false))
	reg := stubRegistry{byName: map[string]*entity.Entity{"users": users, "posts": posts}}
	q := url.Values{"author.c": {"1"}, "author.a": {"1"}, "author.b": {"1"}, "author.d": {"1"}, "author.e": {"1"}}
	for range 30 {
		_, err := parseNestedFiltersValues(q, posts, reg)
		if err == nil || !strings.Contains(err.Error(), `"d"`) {
			t.Fatalf("first refusal = %v, want the one for author.d", err)
		}
		nf, err := parseNestedFiltersValues(url.Values{"author.c": {"1"}, "author.a": {"1"}, "author.b": {"1"}}, posts, reg)
		if err != nil || nf[0].Field != "a" || nf[1].Field != "b" || nf[2].Field != "c" {
			t.Fatalf("filters out of key order: %v %v", nf, err)
		}
	}
}
