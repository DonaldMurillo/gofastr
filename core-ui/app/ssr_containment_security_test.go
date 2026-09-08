package app

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Pins: the SSR pipeline's host-supplied hooks (screen Render/RenderCtx,
// the Load hook, the post-Load ScreenTitle/ScreenLang re-reads) run under
// containment — a panic degrades (SafeRenderCtx fallback / the error
// channel a Load error already takes / the registered title) and never
// escapes the caller. A standalone host wires no recovery middleware, so
// an escaped panic would kill the request with no response. Found by the
// 2026-09-06/07 adversarial round 5 probes (framework/uihost
// ssr_panic_red_test.go); pinned here at the package that owns the
// pipeline. The layout-slot family grammar (layout.go: an errored slot
// renders empty rather than killing the page) is the contract.

// ssrBoomScreen: every render shape panics.
type ssrBoomScreen struct{}

func (ssrBoomScreen) Render() render.HTML { panic("test: screen render boom") }

// ssrBoomCtxScreen: panics through RenderCtx (the dispatch shape
// renderComponentAs prefers for ContextComponent).
type ssrBoomCtxScreen struct{ component.ContextOnly }

func (ssrBoomCtxScreen) RenderCtx(context.Context) render.HTML {
	panic("test: screen ctx render boom")
}

// ssrLoadBoom: Load panics, Render is healthy — a panic can only come from
// the loader hook.
type ssrLoadBoom struct{}

func (c *ssrLoadBoom) Load(context.Context) error { panic("test: screen load boom") }
func (c *ssrLoadBoom) Render() render.HTML        { return render.Text("loaded") }

// ssrTitleBoom: ScreenTitle works on the 1st call (the registration-time
// read) and panics from the 2nd (the post-Load re-read on the per-request
// copy, which starts at calls==1 again).
type ssrTitleBoom struct{ calls int }

func (c *ssrTitleBoom) Render() render.HTML { return render.Text("titled") }
func (c *ssrTitleBoom) ScreenTitle() string {
	c.calls++
	if c.calls > 1 {
		panic("test: screen title boom")
	}
	return "first"
}

// ssrNoPanic runs f and fails with the finding's tag when a panic escapes.
func ssrNoPanic(t *testing.T, tag, what string, f func()) {
	t.Helper()
	escaped := any(nil)
	func() {
		defer func() { escaped = recover() }()
		f()
	}()
	if escaped != nil {
		t.Errorf("SECURITY: [%s] %s: panic %v escaped — the host-supplied hook must run under the SafeRenderCtx containment the layout-slot family pins (an errored hook degrades, it never kills the request)", tag, what, escaped)
	}
}

func TestSSRPipelineContainsHostHookPanics(t *testing.T) {
	ctx := context.Background()

	// Empty-layout full-page arm: RenderPageResult → renderComponentAs.
	// This is the arm a standalone host (no SetDefaultLayout) serves.
	a := NewApp("ssr-plain")
	a.Register("/", ssrBoomScreen{}, nil)
	ssrNoPanic(t, "ssr-containment", "RenderPageResult (no layout) on a panicking screen", func() {
		res, err := a.RenderPageResult(ctx, "/")
		if err != nil {
			t.Errorf("a panicking screen must render its fallback, not error: %v", err)
		} else if !strings.Contains(string(res.HTML), "fui-render-error") {
			t.Errorf("panicking screen must render the SafeRenderCtx fallback box, got:\n%s", res.HTML)
		}
	})

	// RenderCtx dispatch shape on the same arm.
	aCtx := NewApp("ssr-ctx")
	aCtx.Register("/", ssrBoomCtxScreen{}, nil)
	ssrNoPanic(t, "ssr-containment", "RenderPageResult (no layout) on a panicking ctx screen", func() {
		if _, err := aCtx.RenderPageResult(ctx, "/"); err != nil {
			t.Errorf("a panicking ctx screen must render its fallback, not error: %v", err)
		}
	})

	// With-layout control: the chain arm converts to the error channel
	// (its pre-existing containment); it must never panic.
	aCtl := NewApp("ssr-ctl")
	aCtl.SetDefaultLayout(NewLayout("ctl"))
	aCtl.Register("/", ssrBoomScreen{}, nil)
	ssrNoPanic(t, "ssr-containment", "RenderPageResult (with layout) on a panicking screen", func() {
		if _, err := aCtl.RenderPageResult(ctx, "/"); err == nil {
			t.Errorf("with-layout arm must take its error channel on a panicking screen, got success")
		}
	})

	// Intercept-overlay partial arm: RenderOverlayResult renders the same
	// screen through renderComponentAs with a drawer ScreenType.
	aIx := NewApp("ssr-overlay")
	aIx.SetDefaultLayout(NewLayout("ctl"))
	aIx.Register("/", ssrBoomScreen{}, nil)
	ssrNoPanic(t, "ssr-containment", "RenderOverlayResult (drawer) on a panicking screen", func() {
		if _, err := aIx.RenderOverlayResult(ctx, "/", ScreenDrawer); err != nil {
			t.Errorf("overlay arm must render the fallback, not error: %v", err)
		}
	})

	// Screen.Render/RenderCtx (the exported convenience pair) net the same
	// way: fallback, no escape.
	ssrNoPanic(t, "ssr-containment", "Screen.Render on a panicking component", func() {
		s := NewScreen("/x", ssrBoomScreen{})
		if out := string(s.Render()); !strings.Contains(out, "fui-render-error") {
			t.Errorf("Screen.Render must render the fallback box, got:\n%s", out)
		}
	})

	// Load panic: converted to the error channel a Load error already
	// takes — an ERROR return, never an escaped panic.
	aLoad := NewApp("ssr-load")
	aLoad.SetDefaultLayout(NewLayout("ctl"))
	aLoad.Register("/", &ssrLoadBoom{}, nil)
	ssrNoPanic(t, "ssr-loadhook", "RenderPageResult with a panicking Load hook", func() {
		if _, err := aLoad.RenderPageResult(ctx, "/"); err == nil {
			t.Errorf("a panicking Load must take the Load-error channel (error return), got success")
		}
	})
	// The partial path's Load hook takes the same channel.
	aLoadP := NewApp("ssr-load-partial")
	aLoadP.SetDefaultLayout(NewLayout("ctl"))
	aLoadP.Register("/", &ssrLoadBoom{}, nil)
	ssrNoPanic(t, "ssr-loadhook", "RenderPartialResult with a panicking Load hook", func() {
		if _, err := aLoadP.RenderPartialResult(ctx, "/"); err == nil {
			t.Errorf("a panicking Load must take the Load-error channel on the partial path too, got success")
		}
	})

	// ScreenTitle that panics only from its 2nd call (the post-Load
	// re-read): the page renders under the registered title, no escape.
	aTitle := NewApp("ssr-title")
	aTitle.SetDefaultLayout(NewLayout("ctl"))
	aTitle.Register("/", &ssrTitleBoom{}, nil)
	ssrNoPanic(t, "ssr-loadhook", "RenderPageResult with a 2nd-call-panicking ScreenTitle", func() {
		res, err := aTitle.RenderPageResult(ctx, "/")
		if err != nil {
			t.Fatalf("a panicking title re-read must degrade to the registered title, not error: %v", err)
		}
		if !strings.Contains(string(res.HTML), "titled") {
			t.Errorf("title-leg page must still render its content:\n%s", res.HTML)
		}
	})
}

// hookAllowlist exempts a call that LOOKS like the hook shape but is the
// package's own contained delegation. Keyed "<file>:<enclosing func decl>".
var hookAllowlist = map[string]string{
	"screen.go:Render": "(*Screen).Render delegates to (*Screen).RenderCtx, which nets the host render through component.SafeRenderCtx",
}

// TestHostRenderHooksAreNetted scans the non-test sources of core-ui/app
// and core-ui/widget and fails when a host-supplied hook call (a Component
// .Render()/.RenderCtx(...), or a ScreenLoader .Load(ctx)) sits outside
// the containment family: component.SafeRenderCtx/SafeRender, or an
// enclosing function carrying a defer/recover. This is the package-level
// twin of the uihost red probes: it catches a NEW unnetted call site the
// probes never enumerated, in either package, at any depth.
func TestHostRenderHooksAreNetted(t *testing.T) {
	for _, pkgDir := range []string{".", filepath.Join("..", "widget"), filepath.Join("..", "widget", "preset")} {
		files, err := filepath.Glob(filepath.Join(pkgDir, "*.go"))
		if err != nil {
			t.Fatalf("setup broken: scan %s: %v", pkgDir, err)
		}
		for _, path := range files {
			if strings.HasSuffix(path, "_test.go") {
				continue
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				t.Fatalf("setup broken: parse %s: %v", path, err)
			}
			for _, v := range unnettedHookCalls(fset, f, filepath.Base(path)) {
				t.Errorf("SECURITY: [ssr-hook-unnetted] %s:%d %s is a host-supplied hook call outside the containment family — wrap it in component.SafeRenderCtx/SafeRender or a defer/recover helper (allowlist entry %q missing)",
					filepath.Base(path), v.line, v.shape, v.key)
			}
		}
	}
}

type hookSite struct {
	line  int
	shape string
	key   string
	pos   token.Pos
}

// unnettedHookCalls walks one file and reports every hook-shaped call with
// no recover() anywhere inside an enclosing function's source range. The
// in-repo guard shape is a defer/recover helper (or deferred literal)
// wrapping the call — the deferred literal's range lies inside the guarded
// function's range, so range containment matches the idiom exactly.
func unnettedHookCalls(fset *token.FileSet, f *ast.File, file string) []hookSite {
	type fnRange struct {
		pos, end token.Pos
		isDecl   bool
		name     string
	}
	var fns []fnRange
	var recovers []token.Pos
	var hooks []hookSite

	ast.Inspect(f, func(n ast.Node) bool {
		switch v := n.(type) {
		case *ast.FuncDecl:
			fns = append(fns, fnRange{pos: v.Pos(), end: v.End(), isDecl: true, name: v.Name.Name})
		case *ast.FuncLit:
			fns = append(fns, fnRange{pos: v.Pos(), end: v.End(), name: "<literal>"})
		case *ast.CallExpr:
			if id, ok := v.Fun.(*ast.Ident); ok && id.Name == "recover" && len(v.Args) == 0 {
				recovers = append(recovers, v.Pos())
			}
			if sel, ok := v.Fun.(*ast.SelectorExpr); ok {
				if shape, hook := hookShape(sel, v); hook {
					hooks = append(hooks, hookSite{line: fset.Position(v.Pos()).Line, shape: shape, pos: v.Pos()})
				}
			}
		}
		return true
	})

	var out []hookSite
	for _, h := range hooks {
		netted := false
		key := file + ":<toplevel>"
		for _, fn := range fns {
			if fn.pos <= h.pos && h.pos <= fn.end {
				// Ancestor function: a recover anywhere in its range
				// (deferred literal included) nets the call.
				for _, r := range recovers {
					if fn.pos <= r && r <= fn.end {
						netted = true
					}
				}
				if fn.isDecl {
					key = file + ":" + fn.name // innermost decl wins (later ancestors overwrite)
				}
			}
		}
		if _, allowed := hookAllowlist[key]; !netted && !allowed {
			h.key = key
			out = append(out, h)
		}
	}
	return out
}

// hookShape reports whether call is a host-hook call shape.
func hookShape(sel *ast.SelectorExpr, call *ast.CallExpr) (string, bool) {
	switch {
	case sel.Sel.Name == "Render" && len(call.Args) == 0:
		return ".Render()", true
	case sel.Sel.Name == "RenderCtx" && len(call.Args) >= 1:
		return ".RenderCtx(...)", true
	case sel.Sel.Name == "Load" && len(call.Args) == 1:
		// The ScreenLoader signature is Load(ctx); narrow the match to a
		// ctx-named argument so unrelated Load(x) calls stay quiet.
		if id, ok := call.Args[0].(*ast.Ident); ok && id.Name == "ctx" {
			return ".Load(ctx)", true
		}
	}
	return "", false
}
