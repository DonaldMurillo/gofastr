package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/app/decide"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// PagePolicy decides what the render's policy phase decides: a region
// guard's Redirect moves the page, and an eager resolver's failure is
// the page's error, as on RenderPageResult.
func TestPagePolicyMatchesRender(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := guardedShell(app.PolicyFunc(func(context.Context) app.Decision {
		return decide.Redirect("/login")
	}), nil)
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/g", &stubComp2{html: "G"}).Fill(outlets.Aside, &stubComp2{html: "F"}), nil)
	boom := app.NewKey[string]("boom")
	g := app.NewScreenGroup("/e", nil)
	g.Resolve(boom.From(func(context.Context) (string, error) { return "", errors.New("down") }))
	g.Requires(boom)
	g.Screen(app.NewScreen("/e", &stubComp2{html: "E"}), nil)
	a.Router.ScreenGroup(g)

	d, ok, err := a.PagePolicy(context.Background(), "/g")
	if !ok || err != nil || d.Kind != app.DecisionRedirect || d.URL != "/login" {
		t.Errorf("/g: got %+v ok=%v err=%v, want the guard's redirect", d, ok, err)
	}
	if _, ok, err := a.PagePolicy(context.Background(), "/e"); !ok || err == nil {
		t.Errorf("/e: ok=%v err=%v, want the eager resolver's failure", ok, err)
	}
	if _, ok, _ := a.PagePolicy(context.Background(), "/nope"); ok {
		t.Error("/nope: ok for a path no screen matches")
	}
}

// A lazy resolver that a region guard reads fails the page the way an
// eager one does: ErrNotFound read in the policy phase is the render's
// 404, so PagePolicy answers it rather than Allow.
func TestPagePolicyGuardReadNotFound(t *testing.T) {
	a := app.NewApp("t")
	row := app.NewKey[string]("row")
	shell, _ := guardedShell(app.PolicyFunc(func(ctx context.Context) app.Decision {
		_, _ = row.Get(ctx)
		return app.Decision{Kind: app.DecisionAllow}
	}), nil)
	a.SetDefaultLayout(shell)
	a.RegisterScreen(app.NewScreen("/r", &stubComp2{html: "R"}).
		Resolve(row.From(func(context.Context) (string, error) { return "", app.ErrNotFound })), nil)

	if _, err := a.RenderPageResult(context.Background(), "/r"); !errors.Is(err, app.ErrNotFound) {
		t.Fatalf("render err = %v, want ErrNotFound (the premise)", err)
	}
	d, ok, err := a.PagePolicy(context.Background(), "/r")
	if !ok || !errors.Is(err, app.ErrNotFound) {
		t.Errorf("PagePolicy = %+v ok=%v err=%v, want the render's ErrNotFound", d, ok, err)
	}
}

// A pattern-level document about a static page whose policy phase fails
// (here a guard's read of a missing row) withholds: the page answers 404,
// so its doc and its index metadata say nothing about it.
func TestPatternPolicyWithholdsFailedPage(t *testing.T) {
	a := app.NewApp("t")
	row := app.NewKey[string]("row")
	shell, _ := guardedShell(app.PolicyFunc(func(ctx context.Context) app.Decision {
		_, _ = row.Get(ctx)
		return app.Decision{Kind: app.DecisionAllow}
	}), nil)
	a.SetDefaultLayout(shell)
	scr := app.NewScreen("/r", &stubComp2{html: "R"}).
		Resolve(row.From(func(context.Context) (string, error) { return "", app.ErrNotFound }))
	a.RegisterScreen(scr, nil)
	if d := a.PatternPolicy(context.Background(), scr); d.Kind != app.DecisionBlock {
		t.Errorf("PatternPolicy = %+v, want Block", d)
	}
}

// A dynamic pattern whose group Requires a resolver withholds: the
// resolver decides per URL (ErrNotFound for a row the caller may not
// see) and a pattern names none. The same pattern without Requires
// answers its chain's Allow.
func TestPatternPolicyWithholdsDynamicRequires(t *testing.T) {
	a := app.NewApp("t")
	row := app.NewKey[string]("row")
	g := app.NewScreenGroup("/p", nil)
	g.Resolve(row.From(func(context.Context) (string, error) { return "", app.ErrNotFound }))
	g.Requires(row)
	req := app.NewScreen("/p/:id", &paramStub{})
	g.Screen(req, nil)
	a.Router.ScreenGroup(g)
	open := app.NewScreen("/q/:id", &paramStub{})
	a.RegisterScreen(open, nil)

	if d := a.PatternPolicy(context.Background(), req); d.Kind != app.DecisionBlock {
		t.Errorf("/p/:id with Requires: %+v, want Block", d)
	}
	if d := a.PatternPolicy(context.Background(), open); d.Kind != app.DecisionAllow {
		t.Errorf("/q/:id: %+v, want Allow", d)
	}
}

// paramStub is a component for a dynamic route.
type paramStub struct{}

func (*paramStub) Render() render.HTML         { return "" }
func (*paramStub) SetParams(map[string]string) {}
