//go:build red

package uihost

// RED TEST — open finding, 2026-09-06/07 adversarial round 5, phase 2
// (family enumeration; tier T2). Tests-only: no production edits.
//
// Two findings, one file (same family: unnetted host-supplied render hooks
// in the SSR pipeline; the SafeRenderCtx containment layout chrome slots
// get is the pinned family grammar — core-ui/app/layout.go:134-138
// "an errored slot renders empty rather than killing the page").
//
// 1. [ssr-rendercomponent-unnetted]
//    Property: every host-supplied screen Render/RenderCtx runs under
//    SafeRenderCtx containment; uihost serveExpectNoPanic pins this for
//    the 404 and PWA-offline screens ("a standalone host wires no recovery
//    middleware, so the request would die with no response").
//    Surfaces: core-ui/app/app.go::renderComponentAs :686-695 (raw
//    RenderCtx/Render, no recover) reached from RenderPageResult :419-426
//    (empty-layout ScreenPage + every drawer/sheet/dialog arm) and
//    renderPartial :656-658 (intercepted overlay partials); hosts:
//    framework/uihost handlePage :1367, handlePartialPage :2233-2245,
//    embed.go :1004, static builder :85 (export aborts).
//    Finding (probe 2026-09-06): app WITHOUT SetDefaultLayout, GET / on a
//    panicking screen escapes ServeHTTP (behind net/http the request dies
//    with NO response; the static export aborts); the SPA intercept
//    partial (X-Gofastr-Navigate + X-Gofastr-Intercept + X-Gofastr-From)
//    escapes through RenderOverlayResult's drawer arm EVEN WITH a layout.
//    Fix direction: route renderComponentAs's render through
//    component.SafeRenderCtx (render the error/empty), keeping the
//    with-layout and plain-partial arms exactly as contained as today.
//
// 2. [ssr-loadhook-unnetted]
//    Property: the pre-render hooks on the same pipeline (screen Load, and
//    the post-Load ScreenTitle/ScreenLang re-reads) run under the same
//    containment; the di Inject fix on this exact pre-render stage is the
//    precedent ("takes the page down on every request").
//    Surfaces: core-ui/app/app.go::RenderPageResult :396-399 (loader.Load)
//    and :445-460 (titler/langer re-reads); renderPartial :635-639 and
//    :664-666.
//    Finding (probe 2026-09-06): a panicking Load escapes GET / EVEN WITH
//    SetDefaultLayout (the SafeRenderCtx arm is never reached); a
//    ScreenTitle that panics only from its 2nd call (the 1st is the
//    registration-time read, app.go:161-163) escapes the same way after a
//    fully healthy render.
//    Fix direction: recover around the Load hook and the post-Load
//    metadata re-reads, converting to the same error channel a Load
//    *error* already takes (RenderPageResult error → not-found path).

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// redSSRGetNoPanic mirrors serveExpectNoPanic (uihost_security_test.go)
// with per-request headers for the partial/intercept arms. Fails with the
// finding's tag when a screen-side panic escapes ServeHTTP.
func redSSRGetNoPanic(t *testing.T, tag string, ds *UIHost, path string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	escaped := any(nil)
	func() {
		defer func() { escaped = recover() }()
		ds.ServeHTTP(rec, req)
	}()
	if escaped != nil {
		t.Errorf("SECURITY: [%s] screen-side panic %v escaped ServeHTTP for GET %s — "+
			"the host-supplied hook must run under the SafeRenderCtx containment the "+
			"layout-slot family pins (layout.go: an errored slot renders empty rather "+
			"than killing the page); a standalone host wires no recovery middleware, so "+
			"the request dies with no response", tag, escaped, path)
	}
	return rec
}

// redParamPanicScreen: panicScreen's twin for dynamic routes — the
// router refuses to register a dynamic path on a component without
// SetParams (params would be silently dropped), so the intercept arm
// needs a panicking screen that accepts them.
type redParamPanicScreen struct{}

func (redParamPanicScreen) Render() render.HTML         { panic("test: screen render boom") }
func (redParamPanicScreen) SetParams(map[string]string) {}

// TestSSRRenderRedPanicContained: a panicking screen component must not
// escape the request, on the empty-layout full-page arm or the SPA
// intercept-overlay partial arm. Controls pin that the with-layout
// full-page arm and the plain ScreenPage partial stay contained, so the
// red arms fail for the right reason (missing net, not a global one).
func TestSSRRenderRedPanicContained(t *testing.T) {
	// Arm 1: NO SetDefaultLayout — RenderPageResult takes the empty-chain
	// branch (app.go:419-421) → renderComponentAs → raw Render. Escapes.
	a := app.NewApp("red-ssr-render")
	a.Register("/", panicScreen{}, nil)
	redSSRGetNoPanic(t, "ssr-rendercomponent-unnetted", New(a), "/", nil)
	// Arm 2: SPA partial asking for the intercept overlay (header names
	// per uihost.go:1316/2227-2228 and handlePartialPage's dispatch):
	// RenderOverlayResult renders the SAME screen as a drawer through
	// renderComponentAs, which is exactly the unnetted call. Escapes.
	aIx := app.NewApp("red-ssr-intercept")
	aIx.Register("/products", &testHomeComp{}, nil)
	aIx.Register("/products/:id", redParamPanicScreen{}, nil,
		app.InterceptFrom("/products", app.ScreenDrawer))
	redSSRGetNoPanic(t, "ssr-rendercomponent-unnetted", New(aIx), "/products/7", map[string]string{
		"X-Gofastr-Navigate":  "1",
		"X-Gofastr-Intercept": "1",
		"X-Gofastr-From":      "/products",
	})

	// Control A: WITH a default layout the full-page arm renders through
	// SafeRenderCtx — the panic is contained and the request completes.
	aCtl := app.NewApp("red-ssr-render-ctl")
	aCtl.SetDefaultLayout(app.NewLayout("red-ctl"))
	aCtl.Register("/", panicScreen{}, nil)
	redSSRGetNoPanic(t, "ssr-rendercomponent-unnetted", New(aCtl), "/", nil)

	// Control B: a plain ScreenPage partial (no intercept) stays contained
	// (renderPartial's ScreenPage arm already uses SafeRenderCtx).
	aPart := app.NewApp("red-ssr-partial-ctl")
	aPart.SetDefaultLayout(app.NewLayout("red-ctl"))
	aPart.Register("/", panicScreen{}, nil)
	redSSRGetNoPanic(t, "ssr-rendercomponent-unnetted", New(aPart), "/", map[string]string{
		"X-Gofastr-Navigate": "1",
	})
}

// redLoadBoom: Load panics, Render is healthy — the panic can only come
// from the loader hook.
type redLoadBoom struct{}

func (c *redLoadBoom) Load(context.Context) error { panic("test: screen load boom") }
func (c *redLoadBoom) Render() render.HTML        { return render.Text("loaded") }

// redLoadErr: the control twin — Load fails through the error channel.
type redLoadErr struct{}

func (c *redLoadErr) Load(context.Context) error { return context.DeadlineExceeded }
func (c *redLoadErr) Render() render.HTML        { return render.Text("loaded") }

// redTitleBoom: ScreenTitle works on the 1st call (the registration-time
// read, app.go:161-163) and panics from the 2nd (the post-Load re-read).
// newInstance copies the struct, so each request starts at calls==1 and
// panics deterministically on its own re-read.
type redTitleBoom struct{ calls int }

func (c *redTitleBoom) Render() render.HTML { return render.Text("titled") }
func (c *redTitleBoom) ScreenTitle() string {
	c.calls++
	if c.calls >= 2 {
		panic("test: screen title boom")
	}
	return "Red Title"
}

// TestSSRLoadRedPanicContained: the pre-render hooks on the SSR pipeline
// (Load, post-Load ScreenTitle re-read) must not escape the request —
// with a layout wired, since that arm is the one users assume is safe.
func TestSSRLoadRedPanicContained(t *testing.T) {
	// Load panic escapes EVEN WITH SetDefaultLayout: Load runs before the
	// SafeRenderCtx render arm is ever reached.
	a := app.NewApp("red-ssr-load")
	a.SetDefaultLayout(app.NewLayout("red-ctl"))
	a.Register("/", &redLoadBoom{}, nil)
	redSSRGetNoPanic(t, "ssr-loadhook-unnetted", New(a), "/", nil)

	// Control: a Load *error* (not panic) must render the error path —
	// proves the arm is exercised and fails through the error channel.
	aErr := app.NewApp("red-ssr-load-ctl")
	aErr.SetDefaultLayout(app.NewLayout("red-ctl"))
	aErr.Register("/", &redLoadErr{}, nil)
	rec := redSSRGetNoPanic(t, "ssr-loadhook-unnetted", New(aErr), "/", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("control broken: Load error must render the not-found error path (404), got %d — "+
			"without it the Load leg could pass for the wrong reason", rec.Code)
	}

	// ScreenTitle leg: registration's 1st ScreenTitle call must keep
	// working (fail-fast registration stays), while the post-Load re-read
	// on GET / (2nd call on the per-request copy) must not escape.
	aT := app.NewApp("red-ssr-title")
	aT.SetDefaultLayout(app.NewLayout("red-ctl"))
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("setup broken: registration-time ScreenTitle call must not panic: %v", r)
			}
		}()
		aT.Register("/", &redTitleBoom{}, nil)
	}()
	redSSRGetNoPanic(t, "ssr-loadhook-unnetted", New(aT), "/", nil)
}
