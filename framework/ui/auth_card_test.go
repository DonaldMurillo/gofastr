package ui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestAuthCardRendersTitleAndFooter(t *testing.T) {
	h := AuthCard(AuthCardConfig{
		Title:  "Sign in",
		Footer: render.Text("No account?"),
	})
	for _, want := range []string{
		`data-cui-comp="ui-auth-card"`,
		"fui-auth-card__title",
		"Sign in",
		"No account?",
	} {
		mustContain(t, h, want)
	}
}

func TestAuthCardExtraAttrsOnRoot(t *testing.T) {
	h := AuthCard(AuthCardConfig{
		Title:      "Sign in",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
}

// A notice ("Account created. Sign in to continue.") is not a failure:
// it renders as a polite status with its own class, never in the
// danger-styled alert slot.
func TestAuthCardNoticeIsStatus(t *testing.T) {
	h := string(AuthCard(AuthCardConfig{Title: "Sign in", Notice: render.Text("Account created.")}))
	mustContain(t, render.HTML(h), `class="fui-auth-card__notice" role="status"`)
	mustContain(t, render.HTML(h), "Account created.")
	if strings.Contains(h, "fui-auth-card__alert") || strings.Contains(h, `role="alert"`) {
		t.Errorf("a notice alone must not render the alert slot:\n%s", h)
	}
	// A notice is a slot: the internal mark moves to the title and the
	// panel is left for an owner to reach.
	if !regexp.MustCompile(`<h1[^>]*data-cui-internal`).MatchString(h) {
		t.Errorf("a notice-only card must mark the title internal:\n%s", h)
	}
	if regexp.MustCompile(`<div[^>]*fui-auth-card__panel[^>]*data-cui-internal|<div[^>]*data-cui-internal[^>]*fui-auth-card__panel`).MatchString(h) {
		t.Errorf("a notice-only card must leave the panel unmarked:\n%s", h)
	}
}
