package appspec

import (
	"strings"
	"testing"
)

// Pins: login_form/signup_form action is refused at validate time unless it is a safe form action (urlsafe.Anchor).
// (tier T3).
//
// CONTRACT-QUESTION: is silent render-side degradation the contract for
// login_form/signup_form `action` (safe — ui.Form runs Action through the
// html.Form primitive's setURLAttr guard)? The arm's own comment, written
// about the footer hrefs eight lines below, prescribes against exactly this
// shape: "Safe, but silent: the author asked for a link and got one that
// goes nowhere. Reject here instead, the way every sibling problem in this
// validator does. urlsafe.Anchor is the SAME predicate the renderer applies
// … so validate and render cannot disagree" (appspec.go:3236-3242).
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
// Property: within validateAppSpecBlock's login_form/signup_form arm,
// sibling props get the same boundary — `action` must be refused at validate
// time when it is not a safe form action, by the same urlsafe.Anchor
// predicate the renderer applies.
// Surfaces: appspec.go::validateAppSpecBlock :3229-3251 (action
// :3231-3235 is string-type-only; register_href/login_href :3243-3250 are
// refused through urlsafe.Anchor); the value is emitted via
// appSpecAuthFormExpr :6945 as `ui.Form(ui.FormConfig{Action: %q …})`,
// where the guard is render-side only.
// Finding: props action "javascript:alert(1)" (and the case-, data-, and
// vbscript- variants) validateAppSpec cleanly today; the generated login
// screen then silently degrades to a form whose action goes nowhere. The
// author asked for a form and got one that does not post, with no error at
// the only layer that names the screen and prop.
// Fix direction: after the string-type check, refuse with the urlsafe.Anchor
// predicate already imported for the hrefs, so validate and render cannot
// disagree about what a safe form action is.

func TestAuthFormActionRedValidated(t *testing.T) {
	authFormBp := func(action string) AppSpec {
		return AppSpec{
			App: AppSpecApp{Name: "app", Module: "m"},
			Screens: []AppSpecScreen{{
				Name:  "signin",
				Route: "/signin",
				Type:  "page",
				Body:  []AppSpecBlock{{Kind: "login_form", Props: map[string]any{"action": action}}},
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
		if err := ValidateAppSpec(authFormBp(action)); err == nil || !strings.Contains(err.Error(), "action") {
			t.Errorf("SECURITY: [authform-action-validate] login_form action %q accepted by validateAppSpec (err=%v): the arm checks the prop is a string and stops, leaning on silent render-side degradation — the exact shape the arm's own comment rejects for register_href eight lines below (\"Safe, but silent … Reject here instead, the way every sibling problem in this validator does\")", action, err)
		}
	}

	// Controls: the documented shapes stay valid.
	for _, action := range []string{"/auth/login", "https://host/login"} {
		if err := ValidateAppSpec(authFormBp(action)); err != nil {
			t.Fatalf("setup broken: login_form action %q must stay valid: %v", action, err)
		}
	}

	// Contrast leg (green today, proves the arm is reached and the
	// predicate is already in scope): the sibling prop in the same arm IS
	// refused for the same scheme family.
	sib := authFormBp("/auth/login")
	sib.Screens[0].Body[0].Props["register_href"] = "javascript:alert(1)"
	if err := ValidateAppSpec(sib); err == nil || !strings.Contains(err.Error(), "register_href") {
		t.Fatalf("setup broken: sibling register_href refusal moved — the arm this test targets is stale: %v", err)
	}
}
