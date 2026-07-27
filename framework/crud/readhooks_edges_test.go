package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// The failure paths. Each one either fails the request or skips the work, and
// the difference matters: a redaction that errors must never fall through to
// serving the stored value.

func TestWithRealRequestIgnoresNil(t *testing.T) {
	ctx := context.Background()
	if withRealRequest(ctx, nil) != ctx {
		t.Fatal("a nil request must leave the context untouched rather than storing a typed nil " +
			"that requestFrom would then hand to a hook")
	}
}

func TestRunAfterGetReturnsTheHookError(t *testing.T) {
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})
	ch := &CrudHandler{PrimaryKey: "id", Hooks: reg}

	got, err := ch.runAfterGet(WithReadHooks(context.Background()), nil, "r1", map[string]any{"secret": "S"})
	if err == nil {
		t.Fatal("a failing AfterGet must return the error, not the unredacted row")
	}
	if got != nil {
		t.Errorf("the row was returned alongside the error: %#v", got)
	}
}

// A nil node, or one whose target never resolved, is skipped rather than
// dereferenced.
func TestApplyChildReadHooksSkipsNilNodes(t *testing.T) {
	parent := rhEntity(t, "parents")
	seen := 0
	ch := rhHandler(t, parent, "kids", maskingRegistry(&seen))

	rel := entity.HasMany("kids", "kids", "parent_id")
	nodes := []*IncludeNode{nil, {Name: "kids", Relation: rel, Target: nil}}
	rows := []map[string]any{{"id": "p1", "kids": []map[string]any{{"id": "k1"}}}}
	if err := ch.applyChildReadHooks(WithReadHooks(context.Background()), nodes, rows); err != nil {
		t.Fatalf("a nil or unresolved node must be skipped, not error: %v", err)
	}
	if seen != 0 {
		t.Errorf("hooks ran for an unresolved node")
	}
}

// A nil element inside an attachment slice must not reach the hook as a nil
// map, and must not be counted as a row.
func TestApplyChildReadHooksSkipsNilElements(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)
	seen := 0
	ch := rhHandler(t, parent, "kids", maskingRegistry(&seen))

	rows := []map[string]any{{"id": "p1", "kids": []any{nil, map[string]any{"id": "k1", "cardNumber": "4111"}}}}
	if err := ch.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if seen != 1 {
		t.Errorf("hook saw %d rows, want 1 — the nil element should have been skipped", seen)
	}
}

// A hook that drops a row from an include fails the request. Reaching that
// through applyChildReadHooks (rather than reattachHookResults directly) is
// what proves the error is propagated rather than swallowed per node.
func TestApplyChildReadHooksPropagatesReattachFailure(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)

	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.ListPayload)
		if p != nil && len(p.Results) > 0 {
			p.Results = p.Results[:len(p.Results)-1]
		}
		return nil
	})
	ch := rhHandler(t, parent, "kids", reg)

	rows := []map[string]any{{"id": "p1", "kids": []map[string]any{{"id": "k1"}, {"id": "k2"}}}}
	err := ch.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{rhNode(child, rel)}, rows)
	if err == nil {
		t.Fatal("a row-dropping child hook must fail the request rather than serve the rows it dropped")
	}
	if !strings.Contains(err.Error(), "row count") {
		t.Fatalf("error should explain the row count: %v", err)
	}
}

func TestApplyChildReadHooksPropagatesAfterGetError(t *testing.T) {
	child := rhEntity(t, "author")
	rel := entity.BelongsTo("author", "author", "author_id")
	parent := rhEntity(t, "posts", rel)

	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})
	ch := rhHandler(t, parent, "author", reg)

	rows := []map[string]any{{"id": "p1", "author": map[string]any{"id": "a1", "cardNumber": "4111"}}}
	if err := ch.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{rhNode(child, rel)}, rows); err == nil {
		t.Fatal("a failing to-one AfterGet must fail the request, not serve the child raw")
	}
}

// A to-one AfterGet that nils the row is a contract violation, not a redaction.
func TestApplyChildReadHooksRefusesNilledToOneRow(t *testing.T) {
	child := rhEntity(t, "author")
	rel := entity.BelongsTo("author", "author", "author_id")
	parent := rhEntity(t, "posts", rel)

	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.GetPayload)
		if p != nil {
			p.Result = nil
		}
		return nil
	})
	ch := rhHandler(t, parent, "author", reg)

	rows := []map[string]any{{"id": "p1", "author": map[string]any{"id": "a1"}}}
	if err := ch.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{rhNode(child, rel)}, rows); err == nil {
		t.Fatal("nil'ing a to-one child must fail rather than serialise as null")
	}
}

// An error raised at depth surfaces, whether or not the intermediate node had
// hooks of its own.
func TestApplyChildReadHooksPropagatesFromDepth(t *testing.T) {
	grandchild := rhEntity(t, "profiles")
	child := rhEntity(t, "author")
	parent := rhEntity(t, "posts")
	authorRel := entity.BelongsTo("author", "author", "author_id")
	profileRel := entity.HasMany("profiles", "profiles", "author_id")

	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		return errors.New("deep redactor unavailable")
	})
	// Only the GRANDCHILD entity has hooks, so the intermediate node takes the
	// registry-is-nil branch and must still recurse.
	ch := &CrudHandler{
		Entity:     parent,
		PrimaryKey: "id",
		ChildHooks: func(name string) *hook.HookRegistry {
			if name == "profiles" {
				return reg
			}
			return nil
		},
	}

	node := rhNode(child, authorRel)
	node.Children = []*IncludeNode{rhNode(grandchild, profileRel)}
	rows := []map[string]any{{
		"id":     "p1",
		"author": map[string]any{"id": "a1", "profiles": []map[string]any{{"id": "pr1"}}},
	}}
	if err := ch.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{node}, rows); err == nil {
		t.Fatal("an error from a grandchild hook must reach the caller; the intermediate node " +
			"having no hooks of its own does not make it a boundary")
	}
}

// ---- resolveNestedFilters -------------------------------------------------

func TestResolveNestedFiltersRejectsBadSpecs(t *testing.T) {
	target := entity.Define("authors", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "name", Type: schema.String},
			{Name: "secret", Type: schema.String, NoQuery: true},
			{Name: "token", Type: schema.String, Hidden: true},
		},
	})
	posts := entity.Define("posts", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "authors", "author_id")},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "authors": target}}

	cases := map[string]struct {
		spec NestedFilter
		want string
	}{
		"unknown relation":  {NestedFilter{Relation: "nope", Field: "name"}, "unknown relation"},
		"unsafe identifier": {NestedFilter{Relation: "author", Field: "name OR 1=1 --"}, "unsafe"},
		"no_query column":   {NestedFilter{Relation: "author", Field: "secret"}, "cannot be filtered"},
		"hidden column":     {NestedFilter{Relation: "author", Field: "token"}, "not declared"},
		"absent column":     {NestedFilter{Relation: "author", Field: "nope"}, "not declared"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := resolveNestedFilters(posts, reg, []NestedFilter{tc.spec})
			if err == nil {
				t.Fatalf("%s should be refused", name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should contain %q", err, tc.want)
			}
		})
	}
}

func TestResolveNestedFiltersAcceptsAQueryableColumn(t *testing.T) {
	target := entity.Define("authors", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "name", Type: schema.String}},
	})
	posts := entity.Define("posts", entity.EntityConfig{
		Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{entity.BelongsTo("author", "authors", "author_id")},
	})
	reg := stubRegistry{byName: map[string]*entity.Entity{"posts": posts, "authors": target}}

	got, err := resolveNestedFilters(posts, reg, []NestedFilter{
		{Relation: "author", Field: "name", Op: filter.OpIn, Values: []string{"a", "b"}},
	})
	if err != nil {
		t.Fatalf("a queryable column must resolve: %v", err)
	}
	if len(got) != 1 || len(got[0].Values) != 2 {
		t.Fatalf("resolved = %#v", got)
	}
}

// buildExistsSubquery has no error channel, so an unsafe identifier that
// somehow reached it emits an unconditionally-false predicate rather than
// interpolating the name.
func TestBuildExistsSubqueryRefusesUnsafeIdentifiers(t *testing.T) {
	sql, args := buildExistsSubquery("posts", "id", nestedFilter{
		Relation: entity.BelongsTo("author", "authors", "author_id"),
		Field:    "name OR 1=1 --",
		Op:       filter.OpEq,
		Value:    "x",
	})
	if !strings.Contains(sql, "1 = 0") {
		t.Fatalf("an unsafe identifier must produce a false predicate, got: %s", sql)
	}
	if strings.Contains(sql, "1=1") {
		t.Fatalf("the unsafe fragment reached the SQL: %s", sql)
	}
	if len(args) != 0 {
		t.Fatalf("a refused predicate should bind nothing, got %#v", args)
	}
}

// ---- redactEventRecord early returns ---------------------------------------

// An envelope that is not the shape eventData builds passes through rather
// than panicking or dropping the delivery.
func TestRedactEventRecordPassesUnexpectedEnvelopes(t *testing.T) {
	ent := entity.Define("rows", entity.EntityConfig{
		Fields: []schema.Field{{Name: "id", Type: schema.String}},
	})
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error { return nil })
	ch := &CrudHandler{Entity: ent, PrimaryKey: "id", Hooks: reg}
	req := httptest.NewRequest(http.MethodGet, "/rows/_events", nil)

	notAMap := event.Event{Type: event.EntityUpdated, Data: "just a string"}
	if got := ch.redactEventRecord(req, notAMap); got.Data != "just a string" {
		t.Errorf("a non-map payload should pass through, got %#v", got.Data)
	}

	noRecord := event.Event{Type: event.EntityUpdated, Data: map[string]any{"entity": "rows"}}
	got := ch.redactEventRecord(req, noRecord)
	data, _ := got.Data.(map[string]any)
	if data["entity"] != "rows" {
		t.Errorf("an envelope with no record should pass through, got %#v", got.Data)
	}
}

// With no AfterGet registered there is nothing to redact, so the envelope is
// handed on untouched rather than deep-copied per delivery.
func TestRedactEventRecordSkipsWithoutHooks(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "id", Hooks: hook.NewHookRegistry()}
	ev := event.Event{Type: event.EntityUpdated, Data: map[string]any{"record": map[string]any{"id": "r1"}}}
	if got := ch.redactEventRecord(nil, ev); got.Data == nil {
		t.Fatal("the delivery was dropped when there was nothing to redact")
	}
}
