package effect

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Property: the action catalog is closed at every dispatcher. An unknown
// Kind must produce an error, never a silent no-op that reads as success
// to the caller that mounted the hook or route.
//
// world.ValidateAction rejects these at authoring time; this pins the
// re-check in Run and Resolve, the last line of defence the package doc
// promises, including case-variants that no exact-match catalog lookup
// would catch.
func TestUnknownActionKindRefused(t *testing.T) {
	for _, kind := range []string{
		"respond_html", "exec", "redirect", "drop_table",
		"SET_FIELD", "RespondJson", "set_field ",
	} {
		a := world.Action{Kind: kind, Params: map[string]any{}}
		if err := Run(context.Background(), a, Scope{}); err == nil {
			t.Errorf("Run accepted action kind %q", kind)
		}
		if _, err := Resolve(context.Background(), a, Scope{}); err == nil {
			t.Errorf("Resolve accepted action kind %q", kind)
		}
	}
}

// Property: a hook with a hostile Condition fails loud — it never
// silently runs its action, and never silently skips it either.
//
// Conditions are agent-authored expression sources (add_hook over HTTP).
// A condition that errors or evaluates to a non-bool must surface the
// error so a broken gate is visible, not interpreted as pass or fail.
func TestRunHookHostileConditionFailsLoud(t *testing.T) {
	ran := 0
	scope := Scope{
		Entity: map[string]any{"n": "not-a-number"},
		Audit:  func(AuditRecord) { ran++ },
		Emit:   func(EventRecord) { ran++ },
	}
	audit := world.Action{
		Kind:   world.ActionAudit,
		Params: map[string]any{"channel": "c", "message": `"done"`},
	}
	for name, cond := range map[string]string{
		"type error":    "entity.n < 1",
		"non-bool":      "1",
		"unknown ident": "bogus.field == 1",
		"compile error": "entity.n <",
	} {
		h := &world.Hook{ID: "h_" + name, Entity: "e", When: "before_create", Condition: cond, Action: audit}
		if err := RunHook(context.Background(), h, scope); err == nil {
			t.Errorf("%s condition evaluated without error; a broken gate must be loud", name)
		}
		if ran != 0 {
			t.Errorf("%s condition ran the action anyway", name)
		}
	}
	// A false gate runs nothing and reports success; a true gate runs it.
	ran = 0
	h := &world.Hook{ID: "h_false", Entity: "e", When: "before_create", Condition: "false", Action: audit}
	if err := RunHook(context.Background(), h, scope); err != nil {
		t.Fatalf("false condition: %v", err)
	}
	if ran != 0 {
		t.Error("false condition ran the action")
	}
	h = &world.Hook{ID: "h_true", Entity: "e", When: "before_create", Condition: "true", Action: audit}
	if err := RunHook(context.Background(), h, scope); err != nil {
		t.Fatalf("true condition: %v", err)
	}
	if ran != 1 {
		t.Error("true condition did not run the action")
	}
	// nil hook stays a documented no-op.
	if err := RunHook(context.Background(), nil, scope); err != nil {
		t.Errorf("nil hook: %v", err)
	}
}

// Property: set_field can only mutate the entity map it was handed — a
// non-map entity is refused, and no scope root (ctx, result) is
// reachable from the field name or the value expression.
func TestSetFieldConfinedToEntityMap(t *testing.T) {
	action := world.Action{
		Kind:   world.ActionSetField,
		Params: map[string]any{"field": "note", "value": `"set"`},
	}
	for _, hostile := range []any{"plain-id", nil, []any{1}, int64(7)} {
		if err := Run(context.Background(), action, Scope{Entity: hostile, Ctx: map[string]any{}}); err == nil {
			t.Errorf("set_field accepted entity of type %T", hostile)
		}
	}
	ctx := map[string]any{"path": "/keep"}
	entity := map[string]any{"id": int64(1)}
	if err := Run(context.Background(), action, Scope{Entity: entity, Ctx: ctx}); err != nil {
		t.Fatalf("set_field on a map entity failed: %v", err)
	}
	if entity["note"] != "set" {
		t.Errorf("field not written: %v", entity)
	}
	if len(ctx) != 1 || ctx["path"] != "/keep" {
		t.Errorf("ctx mutated by set_field: %v", ctx)
	}
	// A value expression that errors leaves the entity untouched.
	bad := world.Action{
		Kind:   world.ActionSetField,
		Params: map[string]any{"field": "note", "value": "1/0"},
	}
	entity2 := map[string]any{"id": int64(2)}
	if err := Run(context.Background(), bad, Scope{Entity: entity2}); err == nil {
		t.Error("set_field with a failing value expression reported success")
	}
	if _, exists := entity2["note"]; exists {
		t.Error("failing value expression still wrote the field")
	}
}

// Property: audit and emit hostile parameters error cleanly — a hostile
// message/data expression is an error (not a panic, not a silent emit),
// and emit without a topic is refused.
func TestAuditEmitHostileParamsNoPanic(t *testing.T) {
	calls := 0
	scope := Scope{
		Entity: "scalar-entity",
		Audit:  func(AuditRecord) { calls++ },
		Emit:   func(EventRecord) { calls++ },
	}
	for _, a := range []world.Action{
		{Kind: world.ActionAudit, Params: map[string]any{"channel": "c", "message": "entity.field"}},
		{Kind: world.ActionAudit, Params: map[string]any{"channel": "c", "message": "1/0"}},
		{Kind: world.ActionEmitEvent, Params: map[string]any{"data": `"x"`}}, // missing topic
		{Kind: world.ActionEmitEvent, Params: map[string]any{"topic": "t", "data": "entity.x"}},
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s panicked: %v", a.Kind, r)
				}
			}()
			if err := Run(context.Background(), a, scope); err == nil {
				t.Errorf("%s with hostile params reported success", a.Kind)
			}
		}()
	}
	if calls != 0 {
		t.Errorf("hostile params still fired a callback %d times", calls)
	}
	// The working shape keeps firing.
	ok := world.Action{Kind: world.ActionEmitEvent, Params: map[string]any{"topic": "t", "data": `"d"`}}
	if err := Run(context.Background(), ok, scope); err != nil {
		t.Fatalf("valid emit failed: %v", err)
	}
	if calls != 1 {
		t.Errorf("valid emit did not fire: calls=%d", calls)
	}
	// Nil callbacks stay the documented no-op.
	if err := Run(context.Background(), world.Action{Kind: world.ActionAudit, Params: map[string]any{"message": `"m"`}}, Scope{}); err != nil {
		t.Errorf("audit with nil callback: %v", err)
	}
}
