package widget

// Pins: widget chrome callbacks (slot Components, host Skeleton, ExtraCSS)
// render under the containment layout chrome slots get. (2026-09-06/07
// adversarial round 5, phase 2.)
//
// [widget-chrome-unnetted]
// Property: widget chrome callbacks (slot Components, host Skeleton,
// ExtraCSS) render under the containment layout chrome slots get — the
// pinned family grammar in core-ui/app/layout.go:134-138 ("SafeRenderCtx
// ... recovers panics; an errored slot renders empty rather than killing
// the page"). Widget chrome is host-pluggable render hooks of exactly the
// same class, and they are reached on EVERY full-page render a host with
// non-hidden widgets serves (uihost injectWidgetSSR) plus the static
// builder's dumps (:556/:561).
// Surfaces: core-ui/widget/server.go::renderSkeletonCtx :468-477
// (RenderComponentCtx on each slot :471 — raw, no recover; host Skeleton
// :473-474; defaultSkeleton is safe) and core-ui/widget/widget.go::
// RenderCSS :351-356 (ExtraCSS :353-354 raw; serveStyle :423-425 twin).
// Finding (probe 2026-09-06): a panicking slot Component, a panicking
// host Skeleton func, and a panicking ExtraCSS all escape the caller.
// Fix direction: wrap the slot render (component.SafeRenderCtx) and the
// Skeleton/ExtraCSS invocations in a recover that degrades to empty
// slot/chrome-rule — the same containment a layout slot already gets.

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// redChromeBoomSlot: plain Component whose Render panics.
type redChromeBoomSlot struct{}

func (redChromeBoomSlot) Render() render.HTML { panic("test: widget slot boom") }

// redChromeBoomSlotCtx: ctx-aware slot whose RenderCtx panics (the path
// renderSkeletonCtx prefers for per-request chrome).
type redChromeBoomSlotCtx struct{ component.ContextOnly }

func (redChromeBoomSlotCtx) RenderCtx(context.Context) render.HTML {
	panic("test: widget ctx slot boom")
}

// redChromeHealthySlot: control — renders a marker.
type redChromeHealthySlot struct{}

func (redChromeHealthySlot) Render() render.HTML { return render.HTML("RED-CTL-OK") }

// redChromeCallNoPanic runs fn under a recover-guard and fails with the
// finding's tag when the panic escapes (mirrors the uihost
// serveExpectNoPanic grammar).
func redChromeCallNoPanic(t *testing.T, name string, fn func()) {
	t.Helper()
	escaped := any(nil)
	func() {
		defer func() { escaped = recover() }()
		fn()
	}()
	if escaped != nil {
		t.Errorf("SECURITY: [widget-chrome-unnetted] %s: panic %v escaped — "+
			"widget chrome callbacks are host-pluggable render hooks of the same class "+
			"as layout slots, and layout.go pins the family contract (\"an errored slot "+
			"renders empty rather than killing the page\"); uihost inline-injects this "+
			"chrome on every full-page render, so an unnetted hook takes the page down "+
			"with no response", name, escaped)
	}
}

func TestWidgetChromeRedPanicContained(t *testing.T) {
	// (a) Panicking slot Components — both dispatch shapes
	// (renderSkeletonCtx prefers RenderCtx, falls back to Render).
	defSlot := New("red-slot-boom").Slot("body", redChromeBoomSlot{}).Build()
	redChromeCallNoPanic(t, "RenderChrome with panicking plain slot", func() { RenderChrome(&defSlot) })

	defSlotCtx := New("red-slotctx-boom").Slot("body", redChromeBoomSlotCtx{}).Build()
	redChromeCallNoPanic(t, "RenderChromeCtx with panicking ctx slot", func() {
		RenderChromeCtx(context.Background(), &defSlotCtx)
	})

	// (b) Panicking host Skeleton func (healthy slot, so the panic can
	// only come from the Skeleton hook).
	defSkel := New("red-skel-boom").
		Slot("body", redChromeHealthySlot{}).
		Skeleton(func(map[string]render.HTML) render.HTML { panic("test: skeleton boom") }).
		Build()
	redChromeCallNoPanic(t, "RenderChrome with panicking Skeleton", func() { RenderChrome(&defSkel) })

	// (c) Panicking ExtraCSS in the stylesheet dump.
	defCSS := New("red-css-boom").Build()
	defCSS.ExtraCSS = func() string { panic("test: extracss boom") }
	redChromeCallNoPanic(t, "RenderCSS with panicking ExtraCSS", func() { RenderCSS(&defCSS) })

	// Controls: the healthy versions of each surface keep rendering, so
	// the arms above fail for the right reason (missing net, not a broken
	// chrome pipeline).
	ctl := New("red-ctl-chrome").Slot("body", redChromeHealthySlot{}).Build()
	if out := RenderChrome(&ctl); !strings.Contains(out, "RED-CTL-OK") {
		t.Errorf("control broken: healthy slot no longer renders into the chrome:\n%s", out)
	}
	ctlSkel := New("red-ctl-skel").
		Slot("body", redChromeHealthySlot{}).
		Skeleton(func(slots map[string]render.HTML) render.HTML {
			return render.HTML("<div>RED-SKEL-OK " + string(slots["body"]) + "</div>")
		}).
		Build()
	if out := RenderChrome(&ctlSkel); !strings.Contains(out, "RED-SKEL-OK") {
		t.Errorf("control broken: healthy Skeleton no longer renders:\n%s", out)
	}
	ctlCSS := New("red-ctl-css").Build()
	ctlCSS.ExtraCSS = func() string { return "/* red-ctl-css */" }
	if out := RenderCSS(&ctlCSS); !strings.Contains(out, "red-ctl-css") {
		t.Errorf("control broken: healthy ExtraCSS no longer reaches the stylesheet:\n%s", out)
	}
}
