package app_test

// Requires validation (the eager-resolver wiring check): a group
// Requires key with no declaration visible to the screen is a
// silently dropped policy check — Requires(IssueKey) where only
// ProjectKey has a resolver compiles, boots, and never runs the check
// it meant to add. These tests pin the render-time half (the re-check
// that catches a screen registered after mount) and the happy path;
// the mount-time unit twin lives with the other router validations.

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// requiresParamComp is stubComp plus the SetParams a dynamic-route
// screen's component must implement (the router refuses one that
// would silently drop params).
type requiresParamComp struct{ html render.HTML }

func (c *requiresParamComp) Render() render.HTML           { return c.html }
func (c *requiresParamComp) SetParams(m map[string]string) {}

// panicOf runs fn and returns its panic as a string ("" when fn did
// not panic), the shape the validation tests assert against.
func panicOf(fn func()) (got string) {
	defer func() {
		if r := recover(); r != nil {
			got = strings.TrimSpace(panicTextOf(r))
		}
	}()
	fn()
	return ""
}

func panicTextOf(r any) string {
	switch v := r.(type) {
	case string:
		return v
	case error:
		return v.Error()
	default:
		return ""
	}
}

// TestRequiresNameUnknownPanicsAtRender: a screen whose group Requires
// a name nothing declares, registered AFTER the mount-time validation
// ran (so only the render path can catch it), panics on its first
// render — the same re-check contract validateFills honors for fills.
// The dynamic route exercises the router's dynamic arm.
func TestRequiresNameUnknownPanicsAtRender(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	// Mount-time validation over the (empty) registration passes.
	a.Router.ValidateFills()

	// A group joins after mount; its Requires key has no resolver.
	g := app.NewScreenGroup("/late", nil)
	g.Requires(app.NewKey[string]("nope"))
	g.Screen(app.NewScreen("/late/:id", &requiresParamComp{html: "L"}), nil)
	a.Router.ScreenGroup(g)

	got := panicOf(func() { _, _ = a.RenderPageResult(context.Background(), "/late/7") })
	want := `app: group "/late/" requires resolver "nope", but no resolver for that key is visible to "/late/:id"`
	if got != want {
		t.Fatalf("render must refuse the undeclared Requires key:\n got %q\nwant %q", got, want)
	}
}

// TestRequiresNameUnknownPanicsInValidateRequires: the mount-time unit
// half — a group Requires(issue) beside a resolver only for project
// panics when the router validates, with the message that
// names the group prefix and the screen path (the exact-map arm is
// exercised here; the dynamic arm is the render test above).
func TestRequiresNameUnknownPanicsInValidateRequires(t *testing.T) {
	a := app.NewApp("t")
	project := app.NewKey[string]("project")
	issue := app.NewKey[string]("issue")
	g := app.NewScreenGroup("/x", nil)
	g.Resolve(project.From(func(ctx context.Context) (string, error) { return "p", nil }))
	g.Requires(issue) // declared nowhere
	g.Screen(app.NewScreen("/x", &stubComp{html: "X"}), nil)
	a.Router.ScreenGroup(g)

	got := panicOf(func() { a.Router.ValidateRequires() })
	want := `app: group "/x/" requires resolver "issue", but no resolver for that key is visible to "/x"`
	if got != want {
		t.Fatalf("ValidateRequires must refuse the undeclared key:\n got %q\nwant %q", got, want)
	}
}

// TestRequiresNameVisibleRunsEagerly guards the happy path: a correctly
// spelled Requires still expands to the eager policy-phase run (the
// counter is the witness — the screen never reads the resolver).
func TestRequiresNameVisibleRunsEagerly(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	var ran atomic.Int32
	ok := app.NewKey[string]("ok")
	g := app.NewScreenGroup("/ok", nil)
	g.Resolve(ok.From(func(ctx context.Context) (string, error) {
		ran.Add(1)
		return "V", nil
	}))
	g.Requires(ok)
	// The screen never reads the resolver; the eager run must still fire.
	g.Screen(app.NewScreen("/ok", &stubComp{html: "OK"}), nil)
	a.Router.ScreenGroup(g)

	if got := panicOf(func() { a.Router.ValidateRequires() }); got != "" {
		t.Fatalf("a correctly spelled Requires must not panic: %s", got)
	}

	res, err := a.RenderPageResult(context.Background(), "/ok")
	if err != nil {
		t.Fatal(err)
	}
	if ran.Load() != 1 {
		t.Fatalf("eager resolver ran %d times, want 1", ran.Load())
	}
	if !strings.Contains(string(res.HTML), "OK") {
		t.Errorf("the screen must render: %s", res.HTML)
	}
}
