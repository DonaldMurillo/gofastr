package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Property family: a surface the kiln live preview ENFORCES must survive
// graduation into the generated app — the generate-side leg of the property
// kiln/freeze/appspec_hooks_security_test.go pins on the freeze side.
//
// The freeze sibling proves a world's hooks reach the app spec's hooks:
// section. This file asks the next question: what does `gofastr generate` do
// with them? The answer today is nothing — decodeAppSpecHooks parses the
// section (strictly), validateAppSpec never looks at it, and no emitter
// reads bp.Hooks. The handler stub the AppSpecHook doc comment promises
// ("Handler names the func to write"; kiln.md: "freeze emits an owned-Go
// handler stub with a description naming the declarative action") is never
// written, so the before_create validation the operator watched reject bad
// rows in the preview silently does not exist in the shipped app. Endpoints,
// the sibling "declares a surface the generated app must implement in owned
// Go" construct, get a stub in stubs.go AND full validation (handler
// identifier, target entity, method). Hooks get neither.

// hookBp builds a valid app spec with one hook on the tasks entity.
func hookBp(h AppSpecHook) AppSpec {
	return AppSpec{
		App: AppSpecApp{Name: "HookApp", Module: "example.com/hooks", DBDriver: "sqlite", DBURL: "file:hooks.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "tasks",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
		Hooks: []AppSpecHook{h},
	}
}

var validHook = AppSpecHook{
	ID: "tasks_before_create_validate", Entity: "tasks", When: "before_create",
	Handler: "validateTaskTitle", Description: "reject empty titles",
}

// TestHookHandlerMustDeriveIdentifier: a hook's Handler lands in the same
// identifier position an endpoint's Handler does (the func the generated app
// must implement). Every sibling name (entity, field, screen, endpoint
// handler, middleware, plugin, helper, relation) carries an isGoIdentifier
// guard at validate; the hook handler has none, so `handler: "x); PWN() {"`
// is accepted today.
func TestHookHandlerMustDeriveIdentifier(t *testing.T) {
	for _, handler := range []string{
		`x"); PWN() //`,
		"2fast2validate",
		"x`y",
	} {
		bp := hookBp(validHook)
		bp.Hooks[0].Handler = handler
		err := ValidateAppSpec(bp)
		if err == nil || !strings.Contains(err.Error(), "handler") {
			t.Errorf("SECURITY: [hooks] handler %q accepted without an identifier guard (endpoints carry one for the same position): err=%v", handler, err)
		}
	}
	// Control: the guard must not over-fire on the documented shape.
	if err := ValidateAppSpec(hookBp(validHook)); err != nil {
		t.Fatalf("valid hook must validate: %v", err)
	}
}

// TestHookTargetEntityAndWhenMustBeKnown: the live preview rejects a hook
// whose entity is missing or whose `when` is not a lifecycle
// (kiln/render applyHooks: "hook %q: missing entity" / mapHookType error).
// The generator accepts both today, so a typo'd entity or `when: on_save`
// graduates a hook that fires on nothing — the author believes a validation
// is enforced and no such hook exists. Mirrors the endpoint checks
// ("targets unknown entity") and the screen-layout check (unknown enum
// value silently defaulted).
func TestHookTargetEntityAndWhenMustBeKnown(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*AppSpecHook)
	}{
		{"unknown entity", func(h *AppSpecHook) { h.Entity = "nonexistent" }},
		{"empty entity", func(h *AppSpecHook) { h.Entity = "" }},
		{"unknown when", func(h *AppSpecHook) { h.When = "on_save" }},
		{"empty when", func(h *AppSpecHook) { h.When = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp := hookBp(validHook)
			tc.mut(&bp.Hooks[0])
			err := ValidateAppSpec(bp)
			if err == nil {
				t.Fatalf("SECURITY: [hooks] hook with %s accepted; the kiln preview rejects the same shape, so the shipped app enforces a hook that can never fire", tc.name)
			}
		})
	}
	// Control: every lifecycle the preview supports stays legal.
	for _, when := range []string{
		"before_create", "after_create", "before_update", "after_update",
		"before_delete", "after_delete", "before_list", "after_list",
	} {
		bp := hookBp(validHook)
		bp.Hooks[0].When = when
		if err := ValidateAppSpec(bp); err != nil {
			t.Errorf("when=%s must validate: %v", when, err)
		}
	}
}
