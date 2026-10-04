package app_test

// Typed outlet handles: the compile-time guarantee, the mount checks,
// the render-time fill validation, and the per-build inventory
// (docs/DESIGN-layout-outlets.md, "API" and "Validation at mount").
//
//   - TestMistypedOutletDoesNotCompile: a typo'd handle fails the
//     build (the //go:build mistakes fixture beside this file).
//   - TestNewLayoutRefusesSharedHandle: one handle, two layouts — the
//     second NewLayout panics naming both.
//   - TestValidateRefusesBadName: outlet names are wire addresses, not
//     prose; duplicates in one layout are ambiguous.
//   - TestValidateRefusesFillForLayoutNotInChain: a fill for a layout
//     outside the screen's chain panics at render, naming the layout
//     and the outlet.
//   - TestInventoryRefusesDuplicateOutletMarker /
//     TestInventoryRefusesMainFromBuild: each build places its primary
//     and every outlet it owns exactly once and emits no <main> of its
//     own — counted on markers in the output, not on Place calls.

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// mustPanicLike runs fn and returns the recovered panic value, failing
// the test when fn returns normally.
func mustPanicLike(t *testing.T, fn func()) any {
	t.Helper()
	var r any
	func() {
		defer func() { r = recover() }()
		fn()
	}()
	if r == nil {
		t.Fatal("expected a panic")
	}
	return r
}

// TestMistypedOutletDoesNotCompile: the -tags mistakes fixture beside
// this file fills by the string "tolbar", the prototype's addressing;
// the typed-handle API must refuse any string address at compile time. go vet type-checks the package
// including its test files, so this is the design's `go vet -tags
// mistakes` gate.
func TestMistypedOutletDoesNotCompile(t *testing.T) {
	cmd := exec.Command("go", "vet", "-tags", "mistakes", ".")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("go vet -tags mistakes SUCCEEDED — a mistyped outlet handle compiles, the typed-handle guarantee is gone")
	}
	if !strings.Contains(string(out), `cannot use "tolbar"`) {
		t.Fatalf("the compile error must be the string address refused as an outlet:\n%s", out)
	}
}

// TestNewLayoutRefusesSharedHandle: an Outlet belongs to exactly one
// layout; binding one handle into a second NewLayout panics
// naming both layouts.
func TestNewLayoutRefusesSharedHandle(t *testing.T) {
	shared := app.NewOutlet("toolbar")
	build := func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }
	first := app.NewLayout("first", app.LayoutSpec{Outlets: []*app.Outlet{shared}}, build)
	if first == nil {
		t.Fatal("unreachable")
	}
	r := mustPanicLike(t, func() {
		app.NewLayout("second", app.LayoutSpec{Outlets: []*app.Outlet{shared}}, build)
	})
	msg, ok := r.(string)
	if !ok || !strings.Contains(msg, "second") || !strings.Contains(msg, "first") || !strings.Contains(msg, "toolbar") {
		t.Fatalf("panic must name both layouts and the outlet, got: %v", r)
	}
}

// TestValidateRefusesBadName: an outlet name is a wire address
// (data-cui-outlet="<key>#<name>"); anything but letters, digits, '-'
// and '_' panics at NewOutlet, a duplicate name in one layout panics
// at NewLayout, and a valid name passes.
func TestValidateRefusesBadName(t *testing.T) {
	for _, name := range []string{"", "tool bar", "tool#bar", "tool~bar", "tool/bar"} {
		r := mustPanicLike(t, func() { app.NewOutlet(name) })
		if msg, ok := r.(string); !ok || !strings.Contains(msg, name) {
			t.Errorf("NewOutlet(%q) panic must name the outlet, got: %v", name, r)
		}
	}
	// Duplicates inside one layout.
	twice := app.NewOutlet("toolbar")
	r := mustPanicLike(t, func() {
		app.NewLayout("shell", app.LayoutSpec{
			Outlets: []*app.Outlet{twice, app.NewOutlet("toolbar")},
		}, func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() })
	})
	if msg, ok := r.(string); !ok || !strings.Contains(msg, "shell") || !strings.Contains(msg, "toolbar") {
		t.Fatalf("duplicate panic must name the layout and the outlet, got: %v", r)
	}
	// A valid name does not panic.
	if o := app.NewOutlet("tool-bar_2"); o.Name() != "tool-bar_2" {
		t.Errorf("valid name mangled: %q", o.Name())
	}
}

// TestValidateRefusesFillForLayoutNotInChain: a fill whose outlet
// belongs to a layout outside the screen's chain would silently never
// resolve; the render panics instead, naming the layout and the
// outlet. An ownerless handle (never declared by any layout) gets the
// same refusal.
func TestValidateRefusesFillForLayoutNotInChain(t *testing.T) {
	build := func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }

	t.Run("layout not in the chain", func(t *testing.T) {
		shellBToolbar := app.NewOutlet("toolbar")
		_ = app.NewLayout("shell-b", app.LayoutSpec{Outlets: []*app.Outlet{shellBToolbar}}, build)
		a := app.NewApp("t")
		shellA := app.NewLayout("shell-a", app.LayoutSpec{}, build)
		a.SetDefaultLayout(shellA)
		a.RegisterScreen(app.NewScreen("/x", app.NewStaticComponent("X")).
			Fill(shellBToolbar, app.NewStaticComponent("NEVER")), nil)

		r := mustPanicLike(t, func() { _, _ = a.RenderPageResult(context.Background(), "/x") })
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "shell-b") || !strings.Contains(msg, "toolbar") {
			t.Fatalf("panic must name the layout and the outlet, got: %v", r)
		}
	})

	t.Run("handle no layout declares", func(t *testing.T) {
		ghost := app.NewOutlet("ghost")
		a := app.NewApp("t")
		shell := app.NewLayout("shell", app.LayoutSpec{}, build)
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/x", app.NewStaticComponent("X")).
			Fill(ghost, app.NewStaticComponent("NEVER")), nil)

		r := mustPanicLike(t, func() { _, _ = a.RenderPageResult(context.Background(), "/x") })
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "ghost") || !strings.Contains(msg, "no layout declares") {
			t.Fatalf("panic must name the outlet and say no layout declares it, got: %v", r)
		}
	})

	// Control: a fill whose layout IS in the chain renders.
	t.Run("in-chain fill renders", func(t *testing.T) {
		toolbar := app.NewOutlet("toolbar")
		a := app.NewApp("t")
		shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{toolbar}},
			func(ctx context.Context, l *app.LayoutTree) render.HTML {
				return render.Join(l.Place(toolbar), l.Primary())
			})
		a.SetDefaultLayout(shell)
		a.RegisterScreen(app.NewScreen("/x", app.NewStaticComponent("X")).
			Fill(toolbar, app.NewStaticComponent("OK")), nil)
		res, err := a.RenderPageResult(context.Background(), "/x")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(res.HTML), "OK") {
			t.Errorf("in-chain fill must render: %s", res.HTML)
		}
	})
}

// TestInventoryRefusesDuplicateOutletMarker: a build must place each
// outlet it owns exactly once — counted on the MARKER in the output,
// so both a double Place and a component that copies the placed markup
// into a second spot are refused (a call count would miss the copy).
func TestInventoryRefusesDuplicateOutletMarker(t *testing.T) {
	for _, tc := range []struct {
		name  string
		build func(o *app.Outlet) func(ctx context.Context, l *app.LayoutTree) render.HTML
	}{
		{"place called twice", func(o *app.Outlet) func(context.Context, *app.LayoutTree) render.HTML {
			return func(ctx context.Context, l *app.LayoutTree) render.HTML {
				return render.Join(l.Place(o), l.Primary(), l.Place(o))
			}
		}},
		{"markup copied into a second spot", func(o *app.Outlet) func(context.Context, *app.LayoutTree) render.HTML {
			return func(ctx context.Context, l *app.LayoutTree) render.HTML {
				cell := l.Place(o)
				return render.Join(cell, l.Primary(), cell)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := app.NewOutlet("toolbar")
			a := app.NewApp("t")
			shell := app.NewLayout("shell", app.LayoutSpec{Outlets: []*app.Outlet{o}}, tc.build(o))
			a.SetDefaultLayout(shell)
			a.RegisterScreen(app.NewScreen("/", app.NewStaticComponent("HOME")).
				Fill(o, app.NewStaticComponent("TB")), nil)

			_, err := a.RenderPageResult(context.Background(), "/")
			if err == nil {
				t.Fatal("a duplicated outlet marker must be a render error")
			}
			if msg := err.Error(); !strings.Contains(msg, "shell") || !strings.Contains(msg, "toolbar") || !strings.Contains(msg, "2 times") {
				t.Fatalf("error must name the layout, the outlet and the count, got: %v", err)
			}
		})
	}
}

// TestInventoryRefusesMainFromBuild: only the outermost layer's
// Primary owns the document's <main>; a build emitting its own — on
// the outermost layer (two mains) or an inner one (a second main
// under the first) — is a render error naming the layer.
func TestInventoryRefusesMainFromBuild(t *testing.T) {
	ownMain := render.HTML("<main id='not-the-slot'>BAD</main>")
	newApp := func(outerBuild, innerBuild app.LayoutFunc) *app.App {
		a := app.NewApp("t")
		shell := app.NewLayout("shell", app.LayoutSpec{}, outerBuild)
		inner := app.NewLayout("inner", app.LayoutSpec{}, innerBuild)
		a.SetDefaultLayout(shell)
		g := app.NewScreenGroup("/g", inner)
		g.Screen(app.NewScreen("/g/x", app.NewStaticComponent("X")), nil)
		a.Router.ScreenGroup(g)
		return a
	}
	plain := func(ctx context.Context, l *app.LayoutTree) render.HTML { return l.Primary() }

	t.Run("outermost build emits its own main", func(t *testing.T) {
		a := newApp(func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), ownMain)
		}, plain)
		_, err := a.RenderPageResult(context.Background(), "/g/x")
		if err == nil || !strings.Contains(err.Error(), "shell") || !strings.Contains(err.Error(), "<main") {
			t.Fatalf("an outermost build emitting <main> must error naming the layer, got: %v", err)
		}
	})
	t.Run("inner build emits its own main", func(t *testing.T) {
		a := newApp(plain, func(ctx context.Context, l *app.LayoutTree) render.HTML {
			return render.Join(l.Primary(), ownMain)
		})
		_, err := a.RenderPageResult(context.Background(), "/g/x")
		if err == nil || !strings.Contains(err.Error(), "inner") || !strings.Contains(err.Error(), "<main") {
			t.Fatalf("an inner build emitting <main> must error naming the layer (checking only the outermost would pass two mains), got: %v", err)
		}
	})
	t.Run("control: primary alone is fine", func(t *testing.T) {
		a := newApp(plain, plain)
		res, err := a.RenderPageResult(context.Background(), "/g/x")
		if err != nil {
			t.Fatal(err)
		}
		if n := strings.Count(string(res.HTML), "<main"); n != 1 {
			t.Errorf("document carries %d <main> elements, want exactly 1", n)
		}
	})
}
