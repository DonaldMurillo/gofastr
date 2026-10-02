package app_test

// PROTOTYPE (spike/layout-resolve): unit tests for route resolvers
// (docs/DESIGN-layout-outlets.md, "Route resolution"): the one-cell
// memo under concurrent fills, and where an error lands by phase.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// resolveScreen is a stub screen component that reads resolver keys
// in its Load and renders their values (or errors).
type resolveScreen struct {
	keys []*app.Key[string]
	vals []string
}

func (s *resolveScreen) Load(ctx context.Context) error {
	for _, k := range s.keys {
		v, err := k.Get(ctx)
		if err != nil {
			return err
		}
		s.vals = append(s.vals, v)
	}
	return nil
}

func (s *resolveScreen) Render() render.HTML {
	return render.Text("SCREEN[" + strings.Join(s.vals, ",") + "]")
}

// resolveFill is a fill whose Load reads one resolver; on error it
// returns the error (the resolution phases decide containment, not
// the fill).
type resolveFill struct {
	key     *app.Key[string]
	got     string
	loadErr error
}

func (f *resolveFill) Load(ctx context.Context) error {
	v, err := f.key.Get(ctx)
	if err != nil {
		return err
	}
	f.got = v
	return nil
}

func (f *resolveFill) Render() render.HTML { return render.Text("[FILL:" + f.got + "]") }

// TestResolverOnceUnderConcurrentFills pins the one-cell memo: two
// fills load CONCURRENTLY (variant C) and both read the same resolver;
// the body runs exactly once no matter how the goroutines interleave,
// and both renders see the value. The counter is the witness — a
// check-then-run memo races under -race and double-runs here.
func TestResolverOnceUnderConcurrentFills(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	shared := app.NewKey[string]("shared")
	g := app.NewScreenGroup("/x", nil)
	g.Resolve(shared.From(func(ctx context.Context) (string, error) {
		runs.Add(1)
		// Hold the first reader long enough that the sibling fill's
		// read overlaps it: the memo's wait path is the point.
		<-release
		return "V", nil
	}))
	scr := app.NewScreen("/x", &stubComp{html: "X"}).
		Fill(outlets.Toolbar, &resolveFill{key: shared}).
		Fill(outlets.Aside, &resolveFill{key: shared})
	g.Screen(scr, nil)
	a.Router.ScreenGroup(g)

	// The fills' Loads run in goroutines; the first reader parks on
	// <‑release, so the second must WAIT, not re-run. Release from
	// another goroutine once both fills have started reading: the
	// resolver body fires once, its channel close wakes the waiter.
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if runs.Load() == 1 {
				break
			}
			time.Sleep(2 * time.Millisecond)
		}
		close(release)
	}()
	res, err := a.RenderPageResult(context.Background(), "/x")
	if err != nil {
		t.Fatal(err)
	}
	if got := runs.Load(); got != 1 {
		t.Fatalf("resolver body ran %d times, want exactly 1", got)
	}
	if s := string(res.HTML); !strings.Contains(s, "[FILL:V]") {
		t.Errorf("both fills must see the resolved value: %s", s)
	}
	if n := strings.Count(string(res.HTML), "[FILL:V]"); n != 2 {
		t.Errorf("resolved value reached %d fills, want 2", n)
	}
}

// TestResolverErrorByPhase pins where a resolver error lands by the
// phase that first read it: policy (a guard), eager, and Load are
// whole-page outcomes — ErrNotFound the not-found page, anything else
// the error page — while a first read inside a fill is contained to
// that region and logged, the page serving on.
func TestResolverErrorByPhase(t *testing.T) {
	t.Run("policy-read-errors-the-page", func(t *testing.T) {
		a := app.NewApp("t")
		shell, _ := labShell()
		a.SetDefaultLayout(shell)
		boom := errors.New("policy store down")
		key := app.NewKey[string]("boom")
		scr := app.NewScreen("/g", &stubComp{html: "G"}).
			WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
				if _, err := key.Get(ctx); err != nil {
					return app.Decision{Kind: app.DecisionBlock, Status: 500} // unreachable: the page errors first
				}
				return app.Decision{Kind: app.DecisionAllow}
			}))
		scr.Resolve(key.From(func(ctx context.Context) (string, error) { return "", boom }))
		a.RegisterScreen(scr, nil)
		_, err := a.RenderPageResult(context.Background(), "/g")
		var pe *app.PageError
		if !errors.As(err, &pe) {
			t.Fatalf("want *app.PageError, got %v", err)
		}
		if pe.NotFound {
			t.Error("a generic resolver error is not a not-found outcome")
		}
		var re *app.ResolverError
		if !errors.As(err, &re) || re.Name != "boom" {
			t.Errorf("error must name the resolver: %v", err)
		}
	})
	t.Run("not-found-renders-404-outcome", func(t *testing.T) {
		a := app.NewApp("t")
		shell, _ := labShell()
		a.SetDefaultLayout(shell)
		team := app.NewKey[string]("team")
		g := app.NewScreenGroup("/team", nil)
		g.Resolve(team.From(func(ctx context.Context) (string, error) { return "", app.ErrNotFound }))
		g.Requires(team)
		g.Screen(app.NewScreen("/team", &stubComp{html: "T"}), nil)
		a.Router.ScreenGroup(g)
		_, err := a.RenderPageResult(context.Background(), "/team")
		var pe *app.PageError
		if !errors.As(err, &pe) || !pe.NotFound {
			t.Fatalf("ErrNotFound must be a not-found page outcome, got %v", err)
		}
	})
	t.Run("eager-runs-without-a-reader", func(t *testing.T) {
		a := app.NewApp("t")
		shell, _ := labShell()
		a.SetDefaultLayout(shell)
		var ran atomic.Int32
		eager := app.NewKey[string]("eager")
		g := app.NewScreenGroup("/e", nil)
		g.Resolve(eager.From(func(ctx context.Context) (string, error) {
			ran.Add(1)
			return "", errors.New("eager failed")
		}))
		g.Requires(eager)
		// The screen never reads the resolver; the eager run still
		// fails the page.
		g.Screen(app.NewScreen("/e", &stubComp{html: "E"}), nil)
		a.Router.ScreenGroup(g)
		_, err := a.RenderPageResult(context.Background(), "/e")
		var pe *app.PageError
		if !errors.As(err, &pe) {
			t.Fatalf("eager failure must be a page outcome, got %v", err)
		}
		if ran.Load() != 1 {
			t.Fatalf("eager resolver ran %d times, want 1", ran.Load())
		}
	})
	t.Run("load-read-errors-the-page", func(t *testing.T) {
		a := app.NewApp("t")
		shell, _ := labShell()
		a.SetDefaultLayout(shell)
		boom := app.NewKey[string]("boom")
		scr := app.NewScreen("/l", &resolveScreen{keys: []*app.Key[string]{boom}})
		scr.Resolve(boom.From(func(ctx context.Context) (string, error) { return "", errors.New("load store down") }))
		a.RegisterScreen(scr, nil)
		_, err := a.RenderPageResult(context.Background(), "/l")
		var pe *app.PageError
		if !errors.As(err, &pe) {
			t.Fatalf("a Load-phase read must be a page outcome, got %v", err)
		}
	})
	t.Run("fill-read-contained", func(t *testing.T) {
		// A resolver whose FIRST read is the fill's is contained to the
		// region — the phase, not anything else, decides (the spec's
		// rule), and the decided containment agrees.
		logs := captureSlog(t)
		a := app.NewApp("t")
		shell, outlets := labShell()
		a.SetDefaultLayout(shell)
		boom := app.NewKey[string]("boom")
		g := app.NewScreenGroup("/f", nil)
		g.Resolve(boom.From(func(ctx context.Context) (string, error) { return "", errors.New("fill store down") }))
		scr := app.NewScreen("/f", &stubComp{html: "F"}).
			Fill(outlets.Aside, &resolveFill{key: boom})
		g.Screen(scr, nil)
		a.Router.ScreenGroup(g)
		res, err := a.RenderPageResult(context.Background(), "/f")
		if err != nil {
			t.Fatal(err)
		}
		if res.Kind != app.DecisionAllow {
			t.Fatalf("a region-phase read must not fail the page: %v", res.Kind)
		}
		s := string(res.HTML)
		if !strings.Contains(s, "F") {
			t.Errorf("the screen must render: %s", s)
		}
		if !strings.Contains(s, "[HELP]") {
			t.Errorf("the outlet must degrade to its Default: %s", s)
		}
		if !strings.Contains(logs.String(), "fill failed") {
			t.Error("the contained failure must be logged")
		}
	})
	t.Run("panic-becomes-an-error", func(t *testing.T) {
		a := app.NewApp("t")
		shell, _ := labShell()
		a.SetDefaultLayout(shell)
		key := app.NewKey[string]("panic")
		scr := app.NewScreen("/p", &resolveScreen{keys: []*app.Key[string]{key}})
		scr.Resolve(key.From(func(ctx context.Context) (string, error) { panic("resolver exploded") }))
		a.RegisterScreen(scr, nil)
		_, err := a.RenderPageResult(context.Background(), "/p")
		var pe *app.PageError
		if !errors.As(err, &pe) {
			t.Fatalf("a panicking resolver is the error channel a returning error takes: %v", err)
		}
		if !strings.Contains(err.Error(), "resolver exploded") {
			t.Errorf("the recovered panic text must be preserved for the log: %v", err)
		}
	})
	t.Run("part-request-resets-on-policy-phase", func(t *testing.T) {
		a := app.NewApp("t")
		shell, outlets := labShell()
		a.SetDefaultLayout(shell)
		boom := app.NewKey[string]("boom")
		g := app.NewScreenGroup("/pr", nil)
		g.Resolve(boom.From(func(ctx context.Context) (string, error) { return "", errors.New("down") }))
		g.Requires(boom)
		scr := app.NewScreen("/pr", &stubComp{html: "PR"}).
			Fill(outlets.Aside, &resolveFill{key: boom})
		g.Screen(scr, nil)
		a.Router.ScreenGroup(g)
		fill, outcome := a.RenderPartResult(context.Background(), "/pr", "l:shell#aside")
		if outcome != app.PartReset {
			t.Fatalf("a whole-page resolver outcome in a part is the reset, got %d (%q)", outcome, fill.HTML)
		}
	})
	t.Run("outside-a-render-there-is-no-resolver", func(t *testing.T) {
		if _, err := app.NewKey[string]("x").Get(context.Background()); !errors.Is(err, app.ErrNoResolver) {
			t.Fatalf("want ErrNoResolver outside a render, got %v", err)
		}
	})
	t.Run("undeclared-key-is-no-resolver", func(t *testing.T) {
		declared := app.NewKey[string]("declared")
		stray := app.NewKey[string]("stray")
		a := app.NewApp("t")
		shell, _ := labShell()
		a.SetDefaultLayout(shell)
		scr := app.NewScreen("/u", &resolveScreen{keys: []*app.Key[string]{stray}})
		scr.Resolve(declared.From(func(ctx context.Context) (string, error) { return "d", nil }))
		a.RegisterScreen(scr, nil)
		_, err := a.RenderPageResult(context.Background(), "/u")
		if !errors.Is(err, app.ErrNoResolver) || !strings.Contains(err.Error(), `"stray"`) {
			t.Fatalf("reading a key no resolver declared must be ErrNoResolver naming it, got %v", err)
		}
	})
}

// Keys are identities, not names: two keys both called "project" with
// different types each read their own resolver, so two packages that
// pick the same name never share a value.
func TestSameNameKeysStayApart(t *testing.T) {
	a := app.NewApp("t")
	shell, _ := labShell()
	a.SetDefaultLayout(shell)
	byName := app.NewKey[string]("project")
	byID := app.NewKey[int]("project")
	var got string
	var gotID int
	scr := app.NewScreen("/k", &stubComp{html: "K"}).
		WithPolicy(app.PolicyFunc(func(ctx context.Context) app.Decision {
			got, _ = byName.Get(ctx)
			gotID, _ = byID.Get(ctx)
			return app.Decision{Kind: app.DecisionAllow}
		}))
	scr.Resolve(byName.From(func(ctx context.Context) (string, error) { return "billing", nil }))
	scr.Resolve(byID.From(func(ctx context.Context) (int, error) { return 42, nil }))
	a.RegisterScreen(scr, nil)
	if _, err := a.RenderPageResult(context.Background(), "/k"); err != nil {
		t.Fatal(err)
	}
	if got != "billing" || gotID != 42 {
		t.Fatalf("same-name keys shared a resolver: string key read %q, int key read %d", got, gotID)
	}
}

// TestResolverOnceSequentialValue pins the happy path end to end: a
// group resolver feeds a screen's Load and a fill; both see one run's
// value.
func TestResolverOnceSequentialValue(t *testing.T) {
	a := app.NewApp("t")
	shell, outlets := labShell()
	a.SetDefaultLayout(shell)
	v := app.NewKey[string]("v")
	g := app.NewScreenGroup("/v", nil)
	g.Resolve(v.From(func(ctx context.Context) (string, error) {
		return "val", nil
	}))
	scr := app.NewScreen("/v", &resolveScreen{keys: []*app.Key[string]{v}}).
		Fill(outlets.Toolbar, &resolveFill{key: v})
	g.Screen(scr, nil)
	a.Router.ScreenGroup(g)
	res, err := a.RenderPageResult(context.Background(), "/v")
	if err != nil {
		t.Fatal(err)
	}
	s := string(res.HTML)
	if !strings.Contains(s, "SCREEN[val]") || !strings.Contains(s, "[FILL:val]") {
		t.Errorf("screen and fill must both read the resolver: %s", s)
	}
}

var _ = sync.Mutex{} // keep sync imported if subtests above change shape
