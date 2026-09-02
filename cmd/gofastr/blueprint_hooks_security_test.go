package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Property family: a surface the kiln live preview ENFORCES must survive
// graduation into the generated app — the generate-side leg of the property
// kiln/freeze/blueprint_hooks_security_test.go pins on the freeze side.
//
// The freeze sibling proves a world's hooks reach the blueprint's hooks:
// section. This file asks the next question: what does `gofastr generate` do
// with them? The answer today is nothing — decodeBlueprintHooks parses the
// section (strictly), validateBlueprint never looks at it, and no emitter
// reads bp.Hooks. The handler stub the BlueprintHook doc comment promises
// ("Handler names the func to write"; kiln.md: "freeze emits an owned-Go
// handler stub with a description naming the declarative action") is never
// written, so the before_create validation the operator watched reject bad
// rows in the preview silently does not exist in the shipped app. Endpoints,
// the sibling "declares a surface the generated app must implement in owned
// Go" construct, get a stub in stubs.go AND full validation (handler
// identifier, target entity, method). Hooks get neither.

// hookBp builds a valid blueprint with one hook on the tasks entity.
func hookBp(h BlueprintHook) Blueprint {
	return Blueprint{
		App: BlueprintApp{Name: "HookApp", Module: "example.com/hooks", DBDriver: "sqlite", DBURL: "file:hooks.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "tasks",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
		Hooks: []BlueprintHook{h},
	}
}

var validHook = BlueprintHook{
	ID: "tasks_before_create_validate", Entity: "tasks", When: "before_create",
	Handler: "validateTaskTitle", Description: "reject empty titles",
}

// TestBlueprintHooksReachGeneratedApp: a blueprint hook must surface in the
// generated tree — a handler stub like the endpoints get, registry wiring, or
// a refusal. Vanishing silently is the one disallowed outcome, because the
// operator tested against the enforced behavior in the kiln preview.
func TestBlueprintHooksReachGeneratedApp(t *testing.T) {
	bp := hookBp(validHook)
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.content, "validateTaskTitle") || strings.Contains(f.content, "tasks_before_create_validate") {
			return // surfaced: stub, wiring, or refusal — any is fine
		}
	}
	t.Fatalf("SECURITY: [hooks] blueprint hook %q (entity tasks, before_create, handler validateTaskTitle) "+
		"appears in NO generated file: the validation the operator watched in the kiln preview silently "+
		"does not exist in the shipped app. Files: %v", validHook.ID, fileNames(files))
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
		err := validateBlueprint(bp)
		if err == nil || !strings.Contains(err.Error(), "handler") {
			t.Errorf("SECURITY: [hooks] handler %q accepted without an identifier guard (endpoints carry one for the same position): err=%v", handler, err)
		}
	}
	// Control: the guard must not over-fire on the documented shape.
	if err := validateBlueprint(hookBp(validHook)); err != nil {
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
		mut  func(*BlueprintHook)
	}{
		{"unknown entity", func(h *BlueprintHook) { h.Entity = "nonexistent" }},
		{"empty entity", func(h *BlueprintHook) { h.Entity = "" }},
		{"unknown when", func(h *BlueprintHook) { h.When = "on_save" }},
		{"empty when", func(h *BlueprintHook) { h.When = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bp := hookBp(validHook)
			tc.mut(&bp.Hooks[0])
			err := validateBlueprint(bp)
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
		if err := validateBlueprint(bp); err != nil {
			t.Errorf("when=%s must validate: %v", when, err)
		}
	}
}

// TestNavItemIconReachesGeneratedApp: same family, second surface. A nav
// item's `icon:` is an allowed blueprint key, decoded into
// BlueprintNavItem.Icon... and read by nothing on the generate path:
// renderNavItemGo emits Label, Href, Roles and Children, dropping Icon (the
// only other consumer is pack, which writes it back out). The author
// declares an icon, the sidebar renders none, and nothing fails. Either the
// emitter carries it or the decoder refuses it; silent loss is the
// disallowed outcome, exactly as for hooks above.
func TestNavItemIconReachesGeneratedApp(t *testing.T) {
	bp := Blueprint{
		App: BlueprintApp{Name: "IconApp", Module: "example.com/icon", DBDriver: "sqlite", DBURL: "file:icon.db"},
		Nav: []BlueprintNavItem{{Label: "Home", Href: "/", Icon: "home"}},
	}
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.content, `"home"`) {
			return // surfaced
		}
	}
	t.Fatalf("SECURITY: [nav] nav item icon %q decoded but emitted nowhere; the author's icon silently never renders. Files: %v", "home", fileNames(files))
}

// TestBlueprintHooksCompile type-checks a generated app that carries a
// hook: the stub in stubs.go and the HookRegistry registration in
// main.go are emitted Go that assertBlueprintGoParses only parses, and a
// registration against the wrong framework API would parse and still
// fail the operator's first build.
func TestBlueprintHooksCompile(t *testing.T) {
	if testing.Short() {
		t.Skip("compiles a generated module; skipped in -short")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	goVersion, err := repoGoVersion(repoRoot)
	if err != nil {
		t.Fatalf("repoGoVersion: %v", err)
	}
	writeTestFile(t, filepath.Join(dir, "go.mod"),
		"module example.com/demo\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := copyGoSum(repoRoot, dir); err != nil {
		t.Fatalf("copy go.sum: %v", err)
	}
	bp := hookBp(validHook)
	bp.App.Module = "example.com/demo"
	bp.App.OutputDir = "gen"
	if err := validateBlueprint(bp); err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	for _, file := range mustRenderBlueprintFiles(t, bp) {
		full := filepath.Join(dir, "gen", file.name)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(file.content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("go", "vet", "-mod=mod", "./gen/...")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generated app with a hook did not compile: %v\n%s", err, output)
	}
}
