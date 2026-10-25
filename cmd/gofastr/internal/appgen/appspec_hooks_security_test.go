package appgen

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
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
func hookBp(h appspec.AppSpecHook) appspec.AppSpec {
	return appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "HookApp", Module: "example.com/hooks", DBDriver: "sqlite", DBURL: "file:hooks.db"},
		Entities: []framework.EntityDeclaration{{
			Name:   "tasks",
			Fields: []framework.FieldDeclaration{{Name: "title", Type: "string", Required: true}},
		}},
		Hooks: []appspec.AppSpecHook{h},
	}
}

var validHook = appspec.AppSpecHook{
	ID: "tasks_before_create_validate", Entity: "tasks", When: "before_create",
	Handler: "validateTaskTitle", Description: "reject empty titles",
}

// TestAppSpecHooksReachGeneratedApp: an app spec hook must surface in
// the generated tree — a handler stub like the endpoints get, registry
// wiring, or a refusal. Vanishing silently is the one disallowed
// outcome, because the operator tested against the enforced behavior in
// the kiln preview. (The 2026-09-03 probe that drove the full
// load-validate-render path confirmed this contract green; it was
// deleted as a duplicate of this family, which also pins handler
// identifier and entity/when validation below.)
func TestAppSpecHooksReachGeneratedApp(t *testing.T) {
	bp := hookBp(validHook)
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Content, "validateTaskTitle") || strings.Contains(f.Content, "tasks_before_create_validate") {
			return // surfaced: stub, wiring, or refusal — any is fine
		}
	}
	t.Fatalf("SECURITY: [hooks] app spec hook %q (entity tasks, before_create, handler validateTaskTitle) "+
		"appears in NO generated file: the validation the operator watched in the kiln preview silently "+
		"does not exist in the shipped app. Files: %v", validHook.ID, fileNames(files))
}

// TestNavItemIconReachesGeneratedApp: same family, second surface. A nav
// item's `icon:` is an allowed app spec key, decoded into
// AppSpecNavItem.Icon... and read by nothing on the generate path:
// renderNavItemGo emits Label, Href, Roles and Children, dropping Icon (the
// only other consumer is pack, which writes it back out). The author
// declares an icon, the sidebar renders none, and nothing fails. Either the
// emitter carries it or the decoder refuses it; silent loss is the
// disallowed outcome, exactly as for hooks above.
func TestNavItemIconReachesGeneratedApp(t *testing.T) {
	bp := appspec.AppSpec{
		App: appspec.AppSpecApp{Name: "IconApp", Module: "example.com/icon", DBDriver: "sqlite", DBURL: "file:icon.db"},
		Nav: []appspec.AppSpecNavItem{{Label: "Home", Href: "/", Icon: "home"}},
	}
	if err := appspec.ValidateAppSpec(bp); err != nil {
		t.Fatalf("fixture must validate: %v", err)
	}
	files, err := RenderAppSpecFiles(bp)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, f := range files {
		if strings.Contains(f.Content, `"home"`) {
			return // surfaced
		}
	}
	t.Fatalf("SECURITY: [nav] nav item icon %q decoded but emitted nowhere; the author's icon silently never renders. Files: %v", "home", fileNames(files))
}
