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
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// In-package tests for the include fold and the read-hook plumbing. The router
// tests in framework/ prove the wiring; these reach the shapes a router cannot
// easily produce, a []any attachment, a nil row, a hook that returns a
// projection, the same child map attached to two parents.

func rhEntity(t *testing.T, name string, rels ...entity.Relation) *entity.Entity {
	t.Helper()
	return entity.Define(name, entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "card_number", Type: schema.String},
		},
		Relations: rels,
	})
}

// rhHandler builds a handler wired to resolve one child entity's hooks.
func rhHandler(t *testing.T, parent *entity.Entity, childName string, reg *hook.HookRegistry) *CrudHandler {
	t.Helper()
	return &CrudHandler{
		Entity:     parent,
		PrimaryKey: "id",
		ChildHooks: func(name string) *hook.HookRegistry {
			if name == childName {
				return reg
			}
			return nil
		},
	}
}

func rhNode(target *entity.Entity, rel entity.Relation) *IncludeNode {
	return &IncludeNode{Name: rel.Name, Relation: rel, Target: target}
}

// ---- reattachHookResults ---------------------------------------------------

func TestReattachInPlaceIsANoOp(t *testing.T) {
	row := map[string]any{"id": "a", "cardNumber": "****"}
	orig := []map[string]any{row}
	if err := reattachHookResults("kids", "id", orig, []map[string]any{row}); err != nil {
		t.Fatalf("in-place mutation should fold cleanly: %v", err)
	}
	if orig[0]["cardNumber"] != "****" {
		t.Fatalf("row was altered: %#v", orig[0])
	}
}

// A hook that returns a projection, a fresh map with a subset of keys, must
// have its contents folded INTO the attached map, since the parents reference
// that map and replacing the slice would leave them pointing at pre-hook rows.
func TestReattachFoldsProjectionIntoAttachedRow(t *testing.T) {
	row := map[string]any{"id": "a", "cardNumber": "4111", "extra": 1}
	orig := []map[string]any{row}
	proj := []map[string]any{{"id": "a"}}
	if err := reattachHookResults("kids", "id", orig, proj); err != nil {
		t.Fatalf("projection should fold: %v", err)
	}
	if _, still := row["cardNumber"]; still {
		t.Errorf("the dropped key survived in the attached row: %#v", row)
	}
	if row["id"] != "a" {
		t.Errorf("the projection's keys did not land: %#v", row)
	}
}

func TestReattachRefusesRowCountChange(t *testing.T) {
	orig := []map[string]any{{"id": "a"}, {"id": "b"}}
	err := reattachHookResults("kids", "id", orig, []map[string]any{{"id": "a"}})
	if err == nil {
		t.Fatal("dropping a row must fail the request, not quietly serve the rest")
	}
	if !strings.Contains(err.Error(), "row count") {
		t.Fatalf("error should name the row count: %v", err)
	}
}

// A pure permutation is a no-op: the order a client sees comes from the
// attachment the loader built, which reattach never touches, so a child hook
// that sorts p.Results (ordinary list-hook behaviour, correct on the child's
// own route) must not fail the request. What must not happen is a POSITIONAL
// fold, which previously wrote each row's contents into a different parent's
// attachment and, mutating sources mid-loop, produced [C,B,C].
func TestReattachAllowsReorderWithoutCorruption(t *testing.T) {
	a := map[string]any{"id": "a"}
	b := map[string]any{"id": "b"}
	c := map[string]any{"id": "c"}
	orig := []map[string]any{a, b, c}
	if err := reattachHookResults("kids", "id", orig, []map[string]any{c, b, a}); err != nil {
		t.Fatalf("a sorting child hook must not fail the request: %v", err)
	}
	for i, want := range []string{"a", "b", "c"} {
		if orig[i]["id"] != want {
			t.Fatalf("row %d = %v, want %q — the attachment order must be untouched and no row "+
				"may be duplicated or destroyed: %#v", i, orig[i]["id"], want, orig)
		}
	}
}

// Reorder PLUS replacement is the shape that defeated both earlier attempts:
// pointer identity does not recognise a fresh map, so every element looked
// like a positional projection and folded into the wrong parent's row. Matched
// by primary key it is unambiguous.
func TestReattachFoldsReorderedProjections(t *testing.T) {
	a := map[string]any{"id": "a", "secret": "S1"}
	b := map[string]any{"id": "b", "secret": "S2"}
	c := map[string]any{"id": "c", "secret": "S3"}
	orig := []map[string]any{a, b, c}
	// Redact by projection AND sort, both documented, together.
	returned := []map[string]any{
		{"id": "c", "secret": "****"},
		{"id": "b", "secret": "****"},
		{"id": "a", "secret": "****"},
	}
	if err := reattachHookResults("kids", "id", orig, returned); err != nil {
		t.Fatalf("a hook that projects and sorts must fold cleanly: %v", err)
	}
	for _, row := range orig {
		if row["secret"] != "****" {
			t.Errorf("row %v was not redacted: %#v", row["id"], row)
		}
	}
	// Each row kept its own identity, no duplication, no cross-attribution.
	for i, want := range []string{"a", "b", "c"} {
		if orig[i]["id"] != want {
			t.Fatalf("row %d = %v, want %q; the fold mis-attributed a record: %#v",
				i, orig[i]["id"], want, orig)
		}
	}
}

// A replacement that drops its id cannot be matched to a parent.
func TestReattachRefusesProjectionWithoutID(t *testing.T) {
	a := map[string]any{"id": "a", "secret": "S"}
	orig := []map[string]any{a}
	err := reattachHookResults("kids", "id", orig, []map[string]any{{"secret": "****"}})
	if err == nil {
		t.Fatal("a projection that drops the primary key cannot be folded")
	}
	if a["secret"] != "S" {
		t.Errorf("the row was mutated before the refusal: %#v", a)
	}
}

// A row the loader never produced cannot be introduced through the hook.
func TestReattachRefusesUnknownRow(t *testing.T) {
	orig := []map[string]any{{"id": "a"}}
	if err := reattachHookResults("kids", "id", orig, []map[string]any{{"id": "zzz"}}); err == nil {
		t.Fatal("an include must not be able to introduce a row the query did not return")
	}
}

// Returning one row twice would attach it to two parents and drop another.
func TestReattachRefusesDuplicateRow(t *testing.T) {
	a := map[string]any{"id": "a"}
	b := map[string]any{"id": "b"}
	orig := []map[string]any{a, b}
	if err := reattachHookResults("kids", "id", orig, []map[string]any{a, a}); err == nil {
		t.Fatal("the same row returned twice must be refused")
	}
}

func TestReattachRefusesNilRow(t *testing.T) {
	orig := []map[string]any{{"id": "a"}}
	if err := reattachHookResults("kids", "id", orig, []map[string]any{nil}); err == nil {
		t.Fatal("a nil'd row must fail rather than serialise as null")
	}
}

func TestSameMapComparesIdentityNotContents(t *testing.T) {
	a := map[string]any{"id": "x"}
	b := map[string]any{"id": "x"}
	if sameMap(a, b) {
		t.Error("two distinct maps with equal contents must not compare as the same object; " +
			"the fold would then skip a projection it needed to apply")
	}
	if !sameMap(a, a) {
		t.Error("a map must be the same object as itself")
	}
}

func TestFoldHookRowRejectsNil(t *testing.T) {
	if err := foldHookRow("kids", 0, map[string]any{"id": "a"}, nil); err == nil {
		t.Fatal("folding a nil replacement must error")
	}
}

// ---- identityOnly ----------------------------------------------------------

// The degraded write response carries the primary key and nothing else, under
// the JSON casing the rest of the body uses.
func TestIdentityOnlyKeepsOnlyThePrimaryKey(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "id"}
	got := ch.identityOnly(map[string]any{"id": "r1", "secret": "S", "other": 2})
	if len(got) != 1 || got["id"] != "r1" {
		t.Fatalf("identityOnly = %#v, want only the id", got)
	}
}

func TestIdentityOnlyWithoutAPrimaryKeyIsEmpty(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "id"}
	if got := ch.identityOnly(map[string]any{"secret": "S"}); len(got) != 0 {
		t.Fatalf("identityOnly = %#v; with no id present it must not fall back to the row", got)
	}
}

// ---- requestFrom / hookRequest --------------------------------------------

func TestRequestFromPrefersTheInFlightRequest(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "id"}
	real := httptest.NewRequest(http.MethodGet, "/things?role=admin", nil)
	got := ch.requestFrom(withRealRequest(context.Background(), real))
	if got != real {
		t.Fatal("a hook must receive the real request; a synthetic one makes a header-driven " +
			"redactor behave differently one relation hop away")
	}
}

// The synthetic fallback must not carry the read-hook opt-in, or a hook that
// reads through p.Request.Context() re-enters itself.
func TestRequestFromStripsTheOptIn(t *testing.T) {
	ch := &CrudHandler{PrimaryKey: "id"}
	got := ch.requestFrom(WithReadHooks(context.Background()))
	if readHooksEnabled(got.Context()) {
		t.Fatal("the synthetic request carried the opt-in; a hook reading through it recurses")
	}
}

func TestHookRequestLeavesAnOrdinaryRequestAlone(t *testing.T) {
	real := httptest.NewRequest(http.MethodGet, "/things", nil)
	if hookRequest(real) != real {
		t.Fatal("an HTTP request never carries the opt-in, so it must pass through unchanged")
	}
	if hookRequest(nil) != nil {
		t.Fatal("nil must pass through")
	}
}

func TestHookRequestStripsTheOptIn(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/things", nil).
		WithContext(WithReadHooks(context.Background()))
	if readHooksEnabled(hookRequest(r).Context()) {
		t.Fatal("hookRequest must strip the opt-in from the request handed to a hook")
	}
}

// ---- applyChildReadHooks ---------------------------------------------------

// maskingRegistry returns a registry whose AfterList masks cardNumber, and a
// counter of how many rows it saw.
func maskingRegistry(seen *int) *hook.HookRegistry {
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		for i := range p.Results {
			*seen++
			if _, ok := p.Results[i]["cardNumber"]; ok {
				p.Results[i]["cardNumber"] = "****"
			}
		}
		return nil
	})
	return reg
}

// The attachment can be a single object, a []map[string]any, or a []any,
// anything the type switch misses passes through raw, which is the whole bug
// class.
func TestApplyChildReadHooksCoversEveryAttachmentShape(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)

	cases := map[string]func() (any, []map[string]any){
		"single object": func() (any, []map[string]any) {
			m := map[string]any{"id": "k1", "cardNumber": "4111"}
			return m, []map[string]any{m}
		},
		"[]map[string]any": func() (any, []map[string]any) {
			m := map[string]any{"id": "k1", "cardNumber": "4111"}
			return []map[string]any{m}, []map[string]any{m}
		},
		"[]any": func() (any, []map[string]any) {
			m := map[string]any{"id": "k1", "cardNumber": "4111"}
			return []any{m}, []map[string]any{m}
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			attached, want := build()
			seen := 0
			ch := rhHandler(t, parent, "kids", maskingRegistry(&seen))
			rows := []map[string]any{{"id": "p1", "kids": attached}}
			ctx := WithReadHooks(context.Background())
			if err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
				t.Fatalf("applyChildReadHooks: %v", err)
			}
			if want[0]["cardNumber"] != "****" {
				t.Errorf("the %s attachment was not hooked: %#v", name, want[0])
			}
		})
	}
}

// nil and empty attachments must not invoke the hook or panic. Counting ROWS
// cannot show this, an invocation with an empty Results slice leaves a
// per-row counter at zero, so count invocations.
func TestApplyChildReadHooksSkipsEmptyAttachments(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)
	calls := 0
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		calls++
		return nil
	})
	ch := rhHandler(t, parent, "kids", reg)

	rows := []map[string]any{
		{"id": "p1", "kids": nil},
		{"id": "p2", "kids": []map[string]any{}},
		{"id": "p3"},
	}
	ctx := WithReadHooks(context.Background())
	if err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if calls != 0 {
		t.Errorf("the hook was invoked %d times; there were no rows to hook", calls)
	}
}

// One child row reachable from two parents is masked ONCE. A non-idempotent
// hook would otherwise apply twice, and the pointer dedupe is what prevents it.
func TestApplyChildReadHooksDedupesSharedRow(t *testing.T) {
	child := rhEntity(t, "author")
	rel := entity.BelongsTo("author", "author", "author_id")
	parent := rhEntity(t, "posts", rel)

	shared := map[string]any{"id": "a1", "cardNumber": "4111"}
	seen := 0
	// An APPENDING hook, so double application is visible rather than masked
	// by idempotence.
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.ListPayload)
		if !ok {
			return nil
		}
		for i := range p.Results {
			seen++
			p.Results[i]["cardNumber"] = "X"
		}
		return nil
	})
	ch := rhHandler(t, parent, "author", reg)

	rows := []map[string]any{
		{"id": "p1", "author": shared},
		{"id": "p2", "author": shared},
	}
	ctx := WithReadHooks(context.Background())
	if err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if seen != 1 {
		t.Errorf("a shared child row was hooked %d times, want 1", seen)
	}
}

// Without the opt-in on the context nothing runs, the in-process API returns
// stored values, includes included, or code that wrote an included row back
// would persist the mask.
func TestApplyChildReadHooksNeedsTheOptIn(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)
	seen := 0
	ch := rhHandler(t, parent, "kids", maskingRegistry(&seen))

	rows := []map[string]any{{"id": "p1", "kids": []map[string]any{{"id": "k1", "cardNumber": "4111"}}}}
	if err := ch.applyChildReadHooks(context.Background(), []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if seen != 0 {
		t.Errorf("hooks ran without crud.WithReadHooks; in-process reads must stay raw")
	}
}

// A to-one relation serialises as one object, so it also runs the child's
// AfterGet, the hook its own GET /child/{id} route runs.
func TestApplyChildReadHooksRunsAfterGetOnToOne(t *testing.T) {
	child := rhEntity(t, "author")
	parent := rhEntity(t, "posts")

	reg := hook.NewHookRegistry()
	gotID := ""
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok || p.Result == nil {
			return nil
		}
		gotID = p.ID
		p.Result["cardNumber"] = "****"
		return nil
	})
	ch := rhHandler(t, parent, "author", reg)

	for _, rel := range []entity.Relation{
		entity.BelongsTo("author", "author", "author_id"),
		entity.HasOne("author", "author", "post_id"),
	} {
		gotID = ""
		attached := map[string]any{"id": "a1", "cardNumber": "4111"}
		rows := []map[string]any{{"id": "p1", "author": attached}}
		ctx := WithReadHooks(context.Background())
		if err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
			t.Fatalf("applyChildReadHooks: %v", err)
		}
		if attached["cardNumber"] != "****" {
			t.Errorf("to-one include did not run the child's AfterGet: %#v", attached)
		}
		if gotID != "a1" {
			t.Errorf("AfterGet got ID %q, want the child's primary key", gotID)
		}
	}
}

// A to-MANY relation must NOT run AfterGet: its analogue is GET /child, which
// runs AfterList only.
func TestApplyChildReadHooksSkipsAfterGetOnToMany(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)

	reg := hook.NewHookRegistry()
	ran := 0
	reg.RegisterHook(hook.AfterGet, func(ctx context.Context, data any) error {
		ran++
		return nil
	})
	ch := rhHandler(t, parent, "kids", reg)

	rows := []map[string]any{{"id": "p1", "kids": []map[string]any{{"id": "k1"}}}}
	ctx := WithReadHooks(context.Background())
	if err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if ran != 0 {
		t.Errorf("AfterGet ran %d times on a to-many include; its own list route runs AfterList", ran)
	}
}

// A hook error anywhere in the tree fails the request rather than serving a
// partially-hooked payload.
func TestApplyChildReadHooksPropagatesHookError(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)

	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		return errors.New("redactor unavailable")
	})
	ch := rhHandler(t, parent, "kids", reg)

	rows := []map[string]any{{"id": "p1", "kids": []map[string]any{{"id": "k1", "cardNumber": "4111"}}}}
	ctx := WithReadHooks(context.Background())
	err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows)
	if err == nil {
		t.Fatal("a failing child hook must fail the request, not serve the row raw")
	}
}

// Deeper includes are hooked too: the node's rows become the parents for its
// children.
func TestApplyChildReadHooksRecursesIntoGrandchildren(t *testing.T) {
	grandchild := rhEntity(t, "profiles")
	child := rhEntity(t, "author")
	parent := rhEntity(t, "posts")

	authorRel := entity.BelongsTo("author", "author", "author_id")
	profileRel := entity.HasMany("profiles", "profiles", "author_id")

	seen := 0
	ch := rhHandler(t, parent, "profiles", maskingRegistry(&seen))

	node := rhNode(child, authorRel)
	node.Children = []*IncludeNode{rhNode(grandchild, profileRel)}

	deep := map[string]any{"id": "pr1", "cardNumber": "4111"}
	rows := []map[string]any{{
		"id":     "p1",
		"author": map[string]any{"id": "a1", "profiles": []map[string]any{deep}},
	}}
	ctx := WithReadHooks(context.Background())
	if err := ch.applyChildReadHooks(ctx, []*IncludeNode{node}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if deep["cardNumber"] != "****" {
		t.Errorf("a grandchild row was not hooked: %#v", deep)
	}
}

// A node whose target entity has no registry at all is skipped, and recursion
// still reaches its children.
func TestApplyChildReadHooksHandlesMissingRegistry(t *testing.T) {
	child := rhEntity(t, "kids")
	rel := entity.HasMany("kids", "kids", "parent_id")
	parent := rhEntity(t, "parents", rel)

	ch := &CrudHandler{
		Entity:     parent,
		PrimaryKey: "id",
		ChildHooks: func(string) *hook.HookRegistry { return nil },
	}
	rows := []map[string]any{{"id": "p1", "kids": []map[string]any{{"id": "k1"}}}}
	ctx := WithReadHooks(context.Background())
	if err := ch.applyChildReadHooks(ctx, []*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("an entity with no hooks must be skipped, not error: %v", err)
	}
}

// Every read-hook test in the repo used a single-word relation name, so the
// key conversion at the include seam (order_items -> orderItems) was
// unguarded, the same casing-blindness that hid a bug in the admin form one
// package over. With the wrong key the child rows are never found and the
// hook silently runs over nothing.
func TestApplyChildReadHooksConvertsMultiWordRelationName(t *testing.T) {
	child := rhEntity(t, "order_items")
	rel := entity.HasMany("order_items", "order_items", "order_id")
	parent := rhEntity(t, "orders", rel)

	calls := 0
	reg := hook.NewHookRegistry()
	reg.RegisterHook(hook.AfterList, func(ctx context.Context, data any) error {
		p, _ := data.(*hook.ListPayload)
		calls++
		for i := range p.Results {
			p.Results[i]["cardNumber"] = "****"
		}
		return nil
	})
	ch := rhHandler(t, parent, "order_items", reg)
	ch.JSONCase = CaseCamel

	kid := map[string]any{"id": "k1", "cardNumber": "4111"}
	// Attached under the JSON-cased relation name, the way the loader does it.
	rows := []map[string]any{{"id": "o1", "orderItems": []map[string]any{kid}}}
	if err := ch.applyChildReadHooks(WithReadHooks(context.Background()),
		[]*IncludeNode{rhNode(child, rel)}, rows); err != nil {
		t.Fatalf("applyChildReadHooks: %v", err)
	}
	if calls != 1 {
		t.Fatalf("hook invoked %d times, want 1 — a multi-word relation name was not converted, "+
			"so the child rows were never found", calls)
	}
	if kid["cardNumber"] != "****" {
		t.Errorf("the child row was served raw: %#v", kid)
	}
}

// Rows without the primary-key column cannot be matched, so a REPLACEMENT is
// refused, but an in-place edit still folds, since identity is enough there.
func TestReattachWithoutAPrimaryKeyColumn(t *testing.T) {
	a := map[string]any{"label": "x"}
	orig := []map[string]any{a}
	if err := reattachHookResults("kids", "id", orig, []map[string]any{a}); err != nil {
		t.Fatalf("an in-place edit needs no id to fold: %v", err)
	}
	err := reattachHookResults("kids", "id", orig, []map[string]any{{"label": "y"}})
	if err == nil {
		t.Fatal("a replacement cannot be matched when the rows carry no id; it must be refused")
	}
	if !strings.Contains(err.Error(), "no \"id\"") && !strings.Contains(err.Error(), "carry no") {
		t.Fatalf("the error should explain why matching is impossible: %v", err)
	}
}

// An empty pkKey is the same situation.
func TestReattachWithEmptyPKKey(t *testing.T) {
	a := map[string]any{"id": "a"}
	orig := []map[string]any{a}
	if err := reattachHookResults("kids", "", orig, []map[string]any{{"id": "a"}}); err == nil {
		t.Fatal("with no primary-key name there is nothing to match a replacement against")
	}
}

// The fold classifies every element before mutating any, so a payload that is
// going to be refused is left exactly as it was. Without it a valid
// replacement followed by a refused one would have been applied and then
// abandoned, the callers discard the rows on error today, so the damage is
// contained, but the invariant is what makes that containment unnecessary.
func TestReattachDoesNotMutateBeforeARefusal(t *testing.T) {
	a := map[string]any{"id": "a", "secret": "S1"}
	b := map[string]any{"id": "b", "secret": "S2"}
	orig := []map[string]any{a, b}
	returned := []map[string]any{
		{"id": "a", "secret": "****"}, // valid replacement
		{"id": "zzz"},                 // refused: not among the eager-loaded rows
	}
	if err := reattachHookResults("kids", "id", orig, returned); err == nil {
		t.Fatal("an unknown id must be refused")
	}
	if a["secret"] != "S1" || b["secret"] != "S2" {
		t.Errorf("rows were folded before the refusal: %#v %#v", a, b)
	}
}
