//go:build red

package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, fringe wave
// (tier T3).
//
// CONTRACT-QUESTION: is assertBlueprintGoParses an acceptable substitute for
// an emitter-side identifier gate on a path that skips validateBlueprint
// (loadBlueprintPath(path, false), tests, --add fragments)? The file's own
// convention answers no, twice: the endpoint stub loop refuses to emit a
// non-identifier because "assertBlueprintGoParses is the backstop, not the
// guard" (blueprint.go:7327-7336), and the resource-only mount applies the
// "same rule as the hook stubs" for exactly this reachability reason
// (:5600-5608). The middleware/plugins/helpers loops and the screen
// type/mount declarations sit between those two gates with none of their
// own — divergence, not decision.
//
// Pinned sibling (green today): TestBlueprintEndpointStubRefusesNonIdentifiers
// (emitter_quoting_security_test.go) pins the endpoint arm's isGoIdentifier
// skip at both the stub emitter and the app.go registration.
// Property: every blueprint string emitted into Go identifier position is
// gated (skipped) at the emitter whenever render is reachable without
// validateBlueprint — toCamelCase transforms, it does not validate.
// Surfaces: blueprint.go::renderBlueprintStubs — middleware `func
// %sMiddleware` :7345-7350, plugins `type %s struct{}` :7351-7356, helpers
// `func %s()` :7357-7360, all via toCamelCase(item.Name) straight into
// identifier position with no isGoIdentifier gate; ::renderBlueprintApp
// :8027-8032 registers the same ungated names (`fwApp.Use(%sMiddleware)`);
// screen typeName/decls :5228/:5248-5257 and mount declarations
// :5559-5561/:5589-5596 are ungated while the resource-only mount in the
// SAME function (:5598-5608) is guarded.
// Finding: Middleware{Name:"2bad"} — a digit-leading name toCamelCase passes
// through untouched — emits `func 2badMiddleware(next http.Handler)` into
// stubs.go, so renderBlueprintFiles fails WHOLESALE at assertBlueprintGoParses
// (:3637 → :3668) with "generated stubs.go does not parse … Please report
// this with the blueprint that produced it": the documented per-field refusal
// (validateBlueprint rejects the name; the endpoint arm skips it) becomes a
// whole-generate failure with an unactionable message. The same-shaped
// endpoint Handler is skipped and renders fine.
// Fix direction: gate the middleware/plugins/helper stub loops (and the
// app.go registrations, and the screen type/mount declarations) on
// isGoIdentifier exactly as the endpoint, hook, and resource-only-mount arms
// already do: skip, never emit.

func TestStubIdentRedSkipsLikeEndpoints(t *testing.T) {
	// Deterministic: no network in the render path (font fetch is stubbed
	// out the way the audit tests stub it).
	prev := fontFetcher
	fontFetcher = func(string) ([]byte, error) { return nil, fmt.Errorf("offline") }
	t.Cleanup(func() { fontFetcher = prev })

	base := func() Blueprint {
		return Blueprint{
			App: BlueprintApp{Name: "app", Module: "m"},
			Entities: []framework.EntityDeclaration{{
				Name:   "tickets",
				Fields: []framework.FieldDeclaration{{Name: "title", Type: "string"}},
			}},
			Screens: []BlueprintScreen{{Name: "home", Route: "/", Type: "page"}},
		}
	}

	// Leg 1 (finding): a middleware name that survives toCamelCase as a
	// non-identifier must be skipped at the emitter, mirroring the endpoint
	// stub: render succeeds, the identifier never appears.
	bp := base()
	bp.Middleware = []BlueprintNamedStub{{Name: "2bad"}}
	files, err := renderBlueprintFiles(bp)
	if err != nil {
		t.Errorf("SECURITY: [stub-ident-guard-asymmetry] Middleware{Name:%q} fails the WHOLE generate: %v — the endpoint stub loop in the same function is isGoIdentifier-gated and skips (assertBlueprintGoParses is the backstop, not the guard, blueprint.go:7333), but the middleware loop at :7345 emits `func 2badMiddleware(...)` into identifier position. Skip like the endpoint arm, never emit.", "2bad", err)
	} else {
		for _, f := range files {
			if strings.Contains(f.content, "2badMiddleware") {
				t.Errorf("SECURITY: [stub-ident-guard-asymmetry] %s carries the ungated identifier `2badMiddleware` — the endpoint convention skips the same shape at this exact emitter", f.name)
			}
		}
	}

	// Leg 2 (control, green today): the endpoint convention this test pins.
	// The same non-identifier shape as an endpoint Handler is skipped at
	// both the stub emitter and the app.go registration, and the render
	// succeeds. If this leg breaks, the convention moved, not the bug.
	ctrl := base()
	ctrl.Endpoints = []BlueprintEndpoint{{Name: "probe", Method: "GET", Path: "/probe", Handler: "2bad"}}
	cfiles, cerr := renderBlueprintFiles(ctrl)
	if cerr != nil {
		t.Fatalf("setup broken: endpoint Handler %q no longer renders (the pinned skip convention regressed): %v", "2bad", cerr)
	}
	for _, f := range cfiles {
		if strings.Contains(f.content, "2bad") {
			t.Fatalf("setup broken: endpoint control render emitted %q into %s (the pinned skip convention regressed)", "2bad", f.name)
		}
	}
}

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, fringe wave
// (tier T3).
//
// CONTRACT-QUESTION: is silent render-side degradation the contract for
// login_form/signup_form `action` (safe — ui.Form runs Action through the
// html.Form primitive's setURLAttr guard)? The arm's own comment, written
// about the footer hrefs eight lines below, prescribes against exactly this
// shape: "Safe, but silent: the author asked for a link and got one that
// goes nowhere. Reject here instead, the way every sibling problem in this
// validator does. urlsafe.Anchor is the SAME predicate the renderer applies
// … so validate and render cannot disagree" (blueprint.go:3236-3242).
// `action` gets the string-type check the comment's reasoning starts from
// (:3231-3235) and then stops, leaning on the render-side degradation the
// comment exists to reject.
//
// Pinned sibling (green today): TestAuthFormHrefRejectsBadScheme
// (emitter_output_context_security_test.go) pins the render half — the footer
// href goes through ui.Link's urlsafe guard — and the arm itself pins the
// validate half for hrefs; TestFormActionSinksRejectUnsafeURL
// (framework/ui/ui_link_form_security_test.go) documents ui.Form as the
// guarded form sink. This test pins the missing validate half for `action`.
// Property: within validateBlueprintBlock's login_form/signup_form arm,
// sibling props get the same boundary — `action` must be refused at validate
// time when it is not a safe form action, by the same urlsafe.Anchor
// predicate the renderer applies.
// Surfaces: blueprint.go::validateBlueprintBlock :3229-3251 (action
// :3231-3235 is string-type-only; register_href/login_href :3243-3250 are
// refused through urlsafe.Anchor); the value is emitted via
// blueprintAuthFormExpr :6945 as `ui.Form(ui.FormConfig{Action: %q …})`,
// where the guard is render-side only.
// Finding: props action "javascript:alert(1)" (and the case-, data-, and
// vbscript- variants) validateBlueprint cleanly today; the generated login
// screen then silently degrades to a form whose action goes nowhere. The
// author asked for a form and got one that does not post, with no error at
// the only layer that names the screen and prop.
// Fix direction: after the string-type check, refuse with the urlsafe.Anchor
// predicate already imported for the hrefs, so validate and render cannot
// disagree about what a safe form action is.

func TestAuthFormActionRedValidated(t *testing.T) {
	authFormBp := func(action string) Blueprint {
		return Blueprint{
			App: BlueprintApp{Name: "app", Module: "m"},
			Screens: []BlueprintScreen{{
				Name:  "signin",
				Route: "/signin",
				Type:  "page",
				Body:  []BlueprintBlock{{Kind: "login_form", Props: map[string]any{"action": action}}},
			}},
		}
	}

	// Attack legs: every scheme the render guard degrades must be refused
	// at validate, naming `action`.
	for _, action := range []string{
		"javascript:alert(1)",
		"JavaScript:x",
		"data:text/html;x",
		"vbscript:x",
	} {
		if err := validateBlueprint(authFormBp(action)); err == nil || !strings.Contains(err.Error(), "action") {
			t.Errorf("SECURITY: [authform-action-validate] login_form action %q accepted by validateBlueprint (err=%v): the arm checks the prop is a string and stops, leaning on silent render-side degradation — the exact shape the arm's own comment rejects for register_href eight lines below (\"Safe, but silent … Reject here instead, the way every sibling problem in this validator does\")", action, err)
		}
	}

	// Controls: the documented shapes stay valid.
	for _, action := range []string{"/auth/login", "https://host/login"} {
		if err := validateBlueprint(authFormBp(action)); err != nil {
			t.Fatalf("setup broken: login_form action %q must stay valid: %v", action, err)
		}
	}

	// Contrast leg (green today, proves the arm is reached and the
	// predicate is already in scope): the sibling prop in the same arm IS
	// refused for the same scheme family.
	sib := authFormBp("/auth/login")
	sib.Screens[0].Body[0].Props["register_href"] = "javascript:alert(1)"
	if err := validateBlueprint(sib); err == nil || !strings.Contains(err.Error(), "register_href") {
		t.Fatalf("setup broken: sibling register_href refusal moved — the arm this test targets is stale: %v", err)
	}
}
