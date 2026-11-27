package ui_test

// Tooltip behaviour beyond the top layer (#706): the preferred side and
// its fallbacks, dismissal and one-open-at-a-time in every order, touch,
// clicks on the pop, nested tooltips, a skin without the kit's sheet and
// the module's own placement following scroll and clipping.

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// tipMode is how a page places its tooltips.
type tipMode int

const (
	anchoredKit tipMode = iota // the kit's sheet, with anchor positioning
	fallbackKit                // the kit's sheet in a browser without it: the module places
	noKit                      // no sheet at all
)

func (m tipMode) String() string {
	return [...]string{"anchored", "fallback", "no kit sheet"}[m]
}

// noAnchorPositioning makes the page's CSS.supports deny anchor
// positioning, as a browser without it does.
func noAnchorPositioning() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(`(() => { const s = CSS.supports.bind(CSS); CSS.supports = (...a) => /anchor|position-area/.test(a.join(':')) ? false : s(...a); })()`).Do(ctx)
		return err
	})
}

// tooltipBodyPage serves body in the given mode at a w×h viewport and
// waits for headless-tooltip.
func tooltipBodyPage(t *testing.T, mode tipMode, body string, w, h int64, pre ...chromedp.Action) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	head := ""
	switch mode {
	case anchoredKit:
		head = kitHead(t, true)
	case fallbackKit:
		head = kitHead(t, false)
		pre = append(pre, noAnchorPositioning())
	}
	srv := themeTestPageWithHead(t, head, body)
	ctx := moduleTestCtxURL(t, srv.URL, append(pre, emulateFocus(), chromedp.EmulateViewport(w, h))...)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	return ctx
}

func waitOpen(t *testing.T, ctx context.Context, id, why string) {
	t.Helper()
	if !pollJS(ctx, `document.getElementById('`+id+`').matches(':popover-open')`) {
		t.Fatal(why)
	}
	settle()
}

func focusID(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Focus(`#`+id, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
}

func evalJS(t *testing.T, ctx context.Context, js string, out any) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, out)); err != nil {
		t.Fatal(err)
	}
}

// side names where pop sits against trigger, or "" when it is on no one
// side of it (overlapping it).
func side(pop, trigger tipRect) string {
	const slack = 0.5
	switch {
	case pop.Bottom <= trigger.Top+slack:
		return "top"
	case pop.Top >= trigger.Bottom-slack:
		return "bottom"
	case pop.Right <= trigger.Left+slack:
		return "left"
	case pop.Left >= trigger.Right-slack:
		return "right"
	}
	return ""
}

// With room on every side, a tooltip opens on the side it asks for, left
// and right following the reading direction of the tooltip's own
// subtree, whoever places it.
func TestTooltipOpensOnItsPreferredSide(t *testing.T) {
	cases := []struct {
		name      string
		placement ui.TooltipPlacement
		rtl       bool
		want      string
	}{
		{"top", "", false, "top"},
		{"bottom", ui.TooltipBottom, false, "bottom"},
		{"left", ui.TooltipLeft, false, "left"},
		{"right", ui.TooltipRight, false, "right"},
		{"left in a right-to-left subtree", ui.TooltipLeft, true, "right"},
		{"right in a right-to-left subtree", ui.TooltipRight, true, "left"},
	}
	for _, mode := range []tipMode{anchoredKit, fallbackKit} {
		for _, c := range cases {
			t.Run(mode.String()+"/"+c.name, func(t *testing.T) {
				dir := ""
				if c.rtl {
					dir = ` dir="rtl"`
				}
				tip := ui.Tooltip(ui.TooltipConfig{Text: "Opens the settings", ID: "tip", Placement: c.placement},
					render.HTML(`<button id="trigger" type="button">Settings</button>`))
				ctx := tooltipBodyPage(t, mode, `<div`+dir+` style="padding: 220px 0; text-align: center">`+string(tip)+`</div>`, 900, 600)
				hoverID(t, ctx, "trigger")
				waitOpen(t, ctx, "tip", "hover did not open the pop")
				pop, trigger := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
				if got := side(pop, trigger); got != c.want {
					t.Fatalf("the pop %+v is on side %q of the trigger %+v, want %q", pop, got, trigger, c.want)
				}
			})
		}
	}
}

// On a screen too narrow for either side, a left or right tooltip moves
// above or below its trigger instead of covering it.
func TestTooltipLeavesItsTriggerUncoveredOnANarrowScreen(t *testing.T) {
	for _, mode := range []tipMode{anchoredKit, fallbackKit} {
		t.Run(mode.String(), func(t *testing.T) {
			tip := ui.Tooltip(ui.TooltipConfig{Text: "Opens the settings for this workspace", ID: "tip", Placement: ui.TooltipRight},
				render.HTML(`<button id="trigger" type="button">Settings</button>`))
			ctx := tooltipBodyPage(t, mode, `<div style="padding: 200px 0; text-align: center">`+string(tip)+`</div>`, 320, 500)
			hoverID(t, ctx, "trigger")
			waitOpen(t, ctx, "tip", "hover did not open the pop")
			pop, trigger := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
			if side(pop, trigger) == "" {
				t.Fatalf("the pop %+v covers its trigger %+v", pop, trigger)
			}
		})
	}
}

// A tooltip dismissed by Escape stays closed until pointer and focus
// have both left: the pointer leaving and coming back while focus stays
// does not reopen it, nor does focus leaving and coming back while the
// pointer stays.
func TestTooltipDismissalHoldsWhileEitherStays(t *testing.T) {
	open := func(t *testing.T) context.Context {
		ctx := tooltipPage(t, true)
		if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
			t.Fatal("headless-tooltip did not load")
		}
		hoverID(t, ctx, "trigger")
		focusID(t, ctx, "trigger")
		waitOpen(t, ctx, "tip", "hover and focus did not open the pop")
		pressEscape(t, ctx)
		settle()
		if tipOpen(ctx, "tip") {
			t.Fatal("Escape did not close the pop")
		}
		return ctx
	}
	t.Run("the pointer leaves and comes back", func(t *testing.T) {
		ctx := open(t)
		movePointer(t, ctx, 880, 580)
		settle()
		hoverID(t, ctx, "trigger")
		settle()
		if tipOpen(ctx, "tip") {
			t.Fatal("the dismissed pop came back with the pointer while focus never left")
		}
	})
	t.Run("focus leaves and comes back", func(t *testing.T) {
		ctx := open(t)
		focusID(t, ctx, "elsewhere")
		settle()
		focusID(t, ctx, "trigger")
		settle()
		if tipOpen(ctx, "tip") {
			t.Fatal("the dismissed pop came back with focus while the pointer never left")
		}
	})
}

// Opening one tooltip closes the one already open.
func TestTooltipOneOpenAtATime(t *testing.T) {
	ctx := tooltipPage(t, true)
	evalJS(t, ctx, cloneTooltip, nil)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	focusID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "focus did not open the first pop")
	hoverID(t, ctx, "trigger2")
	waitOpen(t, ctx, "tip2", "hover did not open the second pop")
	if tipOpen(ctx, "tip") {
		t.Fatal("the first pop stayed open when the second opened")
	}
}

// Hovered and then focused, a tooltip stays open when focus leaves while
// the pointer stays on its trigger.
func TestTooltipHoverOutlastsFocus(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	focusID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "hover and focus did not open the pop")
	focusID(t, ctx, "elsewhere")
	settle()
	if !tipOpen(ctx, "tip") {
		t.Fatal("the pop closed when focus left while the pointer stayed on the trigger")
	}
}

// Where the module places the pop, it keeps it beside its trigger as the
// page scrolls and keeps it on screen at the viewport's edge.
func TestTooltipFallbackFollowsScrollAndStaysOnScreen(t *testing.T) {
	t.Run("scroll", func(t *testing.T) {
		tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft", ID: "tip"},
			render.HTML(`<button id="trigger" type="button">Save</button>`))
		ctx := tooltipBodyPage(t, fallbackKit, `<div style="height: 2000px; padding-top: 300px; text-align: center">`+string(tip)+`</div>`, 900, 600)
		focusID(t, ctx, "trigger")
		waitOpen(t, ctx, "tip", "focus did not open the pop")
		before, trigger := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
		evalJS(t, ctx, `window.scrollBy(0, 80)`, nil)
		settle()
		after, moved := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
		if math.Abs((moved.Top-trigger.Top)+80) > 1 {
			t.Fatalf("the trigger moved from %v to %v, want 80px up: the page did not scroll", trigger.Top, moved.Top)
		}
		if gap := (after.Top - moved.Top) - (before.Top - trigger.Top); math.Abs(gap) > 1 {
			t.Fatalf("the pop drifted %vpx from its trigger on scroll", gap)
		}
	})
	t.Run("edge", func(t *testing.T) {
		tip := ui.Tooltip(ui.TooltipConfig{Text: "A long description of what this control does", ID: "tip"},
			render.HTML(`<button id="trigger" type="button">?</button>`))
		ctx := tooltipBodyPage(t, fallbackKit, `<div style="padding: 200px 2px 0 0; text-align: right">`+string(tip)+`</div>`, 600, 500)
		hoverID(t, ctx, "trigger")
		waitOpen(t, ctx, "tip", "hover did not open the pop")
		pop := rectOf(t, ctx, "tip")
		var vw float64
		evalJS(t, ctx, `document.documentElement.clientWidth`, &vw)
		if pop.Right > vw-8+0.5 || pop.Left < 8-0.5 {
			t.Fatalf("the pop %+v leaves the 8px edge of a %vpx viewport", pop, vw)
		}
	})
}

// Where the module places the pop, it hides it once its trigger has
// scrolled out of the box that clips it.
func TestTooltipFallbackHidesWithItsClippedTrigger(t *testing.T) {
	tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft", ID: "tip"},
		render.HTML(`<button id="trigger" type="button">Save</button>`))
	ctx := tooltipBodyPage(t, fallbackKit, `<div id="box" style="overflow: auto; height: 120px; width: 300px; margin: 300px 0 0 200px"><div style="height: 400px; padding-top: 40px">`+string(tip)+`</div></div>`, 900, 700)
	focusID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "focus did not open the pop")
	evalJS(t, ctx, `document.getElementById('box').scrollTop = 200`, nil)
	settle()
	var visible bool
	evalJS(t, ctx, `getComputedStyle(document.getElementById('tip')).visibility === 'visible'`, &visible)
	if visible {
		t.Fatal("the pop is still drawn for a trigger its box has scrolled out of view")
	}
}

// A tap on a trigger that cannot take focus shows the tooltip until the
// next tap elsewhere, as the browser's sticky :hover does without script.
func TestTooltipStaysAfterATap(t *testing.T) {
	tip := ui.Tooltip(ui.TooltipConfig{Text: "Ada Lovelace", ID: "tip"},
		render.HTML(`<span id="trigger" style="display: inline-block; width: 40px; height: 40px; background: #888; border-radius: 50%"></span>`))
	ctx := tooltipBodyPage(t, anchoredKit, `<div style="padding: 150px 200px">`+string(tip)+`</div>`, 600, 500)
	tap := func(x, y float64) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			if err := input.DispatchTouchEvent(input.TouchStart, []*input.TouchPoint{{X: x, Y: y}}).Do(ctx); err != nil {
				return err
			}
			return input.DispatchTouchEvent(input.TouchEnd, []*input.TouchPoint{}).Do(ctx)
		})); err != nil {
			t.Fatal(err)
		}
	}
	r := rectOf(t, ctx, "trigger")
	tap((r.Left+r.Right)/2, (r.Top+r.Bottom)/2)
	time.Sleep(600 * time.Millisecond)
	if !tipOpen(ctx, "tip") {
		t.Fatal("the pop did not stay open after a tap on its trigger")
	}
	tap(20, 480)
	settle()
	if tipOpen(ctx, "tip") {
		t.Fatal("the pop stayed open after a tap elsewhere")
	}
}

// A click on an open pop does not reach what the tooltip sits in: inside
// a label it does not toggle the label's checkbox.
func TestTooltipClickOnThePopStaysOnThePop(t *testing.T) {
	tip := ui.Tooltip(ui.TooltipConfig{Text: "Explains the option", ID: "tip", Placement: ui.TooltipBottom},
		render.HTML(`<span id="trigger" tabindex="0">Remember me</span>`))
	ctx := tooltipBodyPage(t, anchoredKit, `<label style="display: block; padding: 150px 200px"><input type="checkbox" id="box"> `+string(tip)+`</label>`, 900, 600)
	hoverID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "hover did not open the pop")
	pop := rectOf(t, ctx, "tip")
	x, y := (pop.Left+pop.Right)/2, (pop.Top+pop.Bottom)/2
	movePointer(t, ctx, x, y)
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y)); err != nil {
		t.Fatal(err)
	}
	settle()
	var checked bool
	evalJS(t, ctx, `document.getElementById('box').checked`, &checked)
	if checked {
		t.Fatal("a click on the pop toggled the checkbox of the label it sits in")
	}
}

// A tooltip in another's trigger: leaving both from the inner one clears
// the outer one's hover, so focus leaving the outer one closes it.
func TestTooltipNestedLetsGoOfTheOuter(t *testing.T) {
	inner := ui.Tooltip(ui.TooltipConfig{Text: "Inner help", ID: "inner", Placement: ui.TooltipBottom},
		render.HTML(`<button id="inner-trigger" type="button">Inner</button>`))
	outer := ui.Tooltip(ui.TooltipConfig{Text: "Outer help", ID: "outer"},
		render.HTML(`<span id="outer-trigger" tabindex="0" style="display: inline-block; padding: 20px; background: #eee">Outer `+string(inner)+`</span>`))
	ctx := tooltipBodyPage(t, anchoredKit, `<div style="padding: 150px 200px">`+string(outer)+`</div><button id="elsewhere" type="button" style="margin: 200px">Elsewhere</button>`, 1000, 700)
	o := rectOf(t, ctx, "outer-trigger")
	movePointer(t, ctx, o.Left+4, o.Top+4)
	settle()
	hoverID(t, ctx, "inner-trigger")
	settle()
	movePointer(t, ctx, 990, 690)
	settle()
	focusID(t, ctx, "outer-trigger")
	waitOpen(t, ctx, "outer", "focus did not open the outer pop")
	focusID(t, ctx, "elsewhere")
	settle()
	if tipOpen(ctx, "outer") {
		t.Fatal("the outer pop stayed open with focus and the pointer both elsewhere")
	}
}

// An Escape another handler has already taken (preventDefault) is not
// the tooltip's: the open pop stays.
func TestTooltipLeavesAnEscapeAlreadyTaken(t *testing.T) {
	taker := chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(`document.addEventListener('keydown', (e) => { if (e.key === 'Escape' && window.__takeEscape) e.preventDefault(); }, true)`).Do(ctx)
		return err
	})
	ctx := tooltipPage(t, true, taker)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "hover did not open the pop")
	evalJS(t, ctx, `window.__takeEscape = true`, nil)
	pressEscape(t, ctx)
	settle()
	if !tipOpen(ctx, "tip") {
		t.Fatal("the pop closed on an Escape another handler had already taken")
	}
}

// headless.Tooltip in a skin without the kit's sheet: the module places
// the pop beside its trigger, not where the browser puts a bare popover.
func TestHeadlessTooltipWithoutTheKitSheetSitsBesideItsTrigger(t *testing.T) {
	tip := headless.Tooltip(headless.TooltipProps{Text: "Saves the draft", ID: "tip",
		Trigger: render.HTML(`<button id="trigger" type="button">Save</button>`)}, nil)
	ctx := tooltipBodyPage(t, noKit, `<div style="padding: 40px">`+string(tip)+`</div>`, 900, 600)
	hoverID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "hover did not open the pop")
	pop, trigger := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
	if s := side(pop, trigger); s != "top" && s != "bottom" {
		t.Fatalf("the pop %+v is not above or below its trigger %+v", pop, trigger)
	}
	if mid := (pop.Left + pop.Right) / 2; mid < trigger.Left || mid > trigger.Right {
		t.Fatalf("the pop's centre %v is not over its trigger (%v..%v)", mid, trigger.Left, trigger.Right)
	}
}

// A pop over a neighbouring control: a click on it dismisses the tooltip,
// so the next click reaches the control beneath.
func TestTooltipClickOnThePopGetsItOutOfTheWay(t *testing.T) {
	row := func(n string) string {
		tip := ui.Tooltip(ui.TooltipConfig{Text: "Edits this row and everything in it", ID: "tip" + n},
			render.HTML(`<button id="edit`+n+`" type="button" onclick="window.__clicks`+n+` = (window.__clicks`+n+` || 0) + 1">Edit</button>`))
		return `<div style="height: 32px; display: flex; align-items: center">` + string(tip) + `</div>`
	}
	ctx := tooltipBodyPage(t, anchoredKit, `<div style="padding: 150px 300px">`+row("1")+row("2")+`</div>`, 900, 600)
	hoverID(t, ctx, "edit2")
	waitOpen(t, ctx, "tip2", "hover did not open the second row's pop")
	target, pop := rectOf(t, ctx, "edit1"), rectOf(t, ctx, "tip2")
	x, y := (target.Left+target.Right)/2, (target.Top+target.Bottom)/2
	if x < pop.Left || x > pop.Right || y < pop.Top || y > pop.Bottom {
		t.Fatalf("the second row's pop %+v does not cover the first row's button %+v, so the test proves nothing", pop, target)
	}
	movePointer(t, ctx, x, y)
	settle()
	if !tipOpen(ctx, "tip2") {
		t.Fatal("the pop closed when the pointer moved onto it")
	}
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y)); err != nil {
		t.Fatal(err)
	}
	settle()
	if tipOpen(ctx, "tip2") {
		t.Fatal("a click on the pop left it open over the control beneath")
	}
	if err := chromedp.Run(ctx, chromedp.MouseClickXY(x, y)); err != nil {
		t.Fatal(err)
	}
	settle()
	var clicks int
	evalJS(t, ctx, `window.__clicks1 || 0`, &clicks)
	if clicks != 1 {
		t.Fatalf("the first row's button got %d clicks after the pop was clicked away, want 1", clicks)
	}
}

// With forced colours the pop keeps a visible edge: the background turns
// to Canvas, so without a border the text would float loose.
func TestTooltipKeepsAnEdgeInForcedColours(t *testing.T) {
	forced := emulation.SetEmulatedMedia().WithFeatures([]*emulation.MediaFeature{{Name: "forced-colors", Value: "active"}})
	tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft", ID: "tip"},
		render.HTML(`<button id="trigger" type="button">Save</button>`))
	ctx := tooltipBodyPage(t, anchoredKit, `<div style="padding: 200px; text-align: center">`+string(tip)+`</div>`, 900, 600, forced)
	hoverID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "hover did not open the pop")
	var edge bool
	evalJS(t, ctx, `(() => { const s = getComputedStyle(document.getElementById('tip')); return s.borderTopStyle !== 'none' && parseFloat(s.borderTopWidth) >= 1; })()`, &edge)
	if !edge {
		t.Fatal("the open pop has no border under forced colours")
	}
}

// Long text wraps inside the pop and stays on a phone's screen.
func TestTooltipLongTextWrapsOnAPhone(t *testing.T) {
	for _, mode := range []tipMode{anchoredKit, fallbackKit} {
		t.Run(mode.String(), func(t *testing.T) {
			tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft to your workspace without closing the editor, so you can keep writing", ID: "tip"},
				render.HTML(`<button id="trigger" type="button">Save</button>`))
			ctx := tooltipBodyPage(t, mode, `<div style="padding: 200px 0; text-align: center">`+string(tip)+`</div>`, 375, 600)
			hoverID(t, ctx, "trigger")
			waitOpen(t, ctx, "tip", "hover did not open the pop")
			pop := rectOf(t, ctx, "tip")
			var spill bool
			evalJS(t, ctx, `(() => { const e = document.getElementById('tip'); return e.scrollWidth > e.clientWidth + 1; })()`, &spill)
			if spill || pop.Left < 0 || pop.Right > 375 {
				t.Fatalf("the pop %+v spills its text (%v) or leaves a 375px screen", pop, spill)
			}
		})
	}
}

// A pop hidden by other code (hidePopover, a swap that strips popover)
// does not swallow the next Escape.
func TestTooltipEscapePassesWhenItsPopIsAlreadyHidden(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "hover did not open the pop")
	evalJS(t, ctx, `window.__escSeen = 0; document.addEventListener('keydown', e => { if (e.key === 'Escape') window.__escSeen++; }); document.getElementById('tip').hidePopover()`, nil)
	pressEscape(t, ctx)
	var seen int
	evalJS(t, ctx, `window.__escSeen`, &seen)
	if seen != 1 {
		t.Fatalf("after other code hid the pop, the page saw %d Escape presses, want 1", seen)
	}
}

// A tooltip open by focus comes back when another, opened by hover over
// it, closes as the pointer leaves, since focus never left its trigger.
func TestTooltipFocusedOneComesBackAfterAHover(t *testing.T) {
	ctx := tooltipPage(t, true)
	evalJS(t, ctx, cloneTooltip, nil)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	focusID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "focus did not open the first pop")
	hoverID(t, ctx, "trigger2")
	waitOpen(t, ctx, "tip2", "hover did not open the second pop")
	movePointer(t, ctx, 880, 580)
	settle()
	if tipOpen(ctx, "tip2") || !tipOpen(ctx, "tip") {
		t.Fatalf("after the pointer left: hovered pop open %v, focused pop open %v; want closed and open", tipOpen(ctx, "tip2"), tipOpen(ctx, "tip"))
	}
}

// The gap between pop and trigger comes from the sheet's spacing token,
// whoever places the pop, so the module does not keep its own copy.
func TestTooltipGapFollowsTheSpacingToken(t *testing.T) {
	for _, mode := range []tipMode{anchoredKit, fallbackKit} {
		t.Run(mode.String(), func(t *testing.T) {
			tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft", ID: "tip", Placement: ui.TooltipBottom},
				render.HTML(`<button id="trigger" type="button">Save</button>`))
			ctx := tooltipBodyPage(t, mode, `<style>:root { --spacing-sm: 10px; }</style><div style="padding: 200px 0; text-align: center">`+string(tip)+`</div>`, 900, 600)
			hoverID(t, ctx, "trigger")
			waitOpen(t, ctx, "tip", "hover did not open the pop")
			pop, trigger := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
			if gap := pop.Top - trigger.Bottom; math.Abs(gap-15) > 1 {
				t.Fatalf("the pop sits %vpx below its trigger, want 15 (1.5 × a 10px --spacing-sm)", gap)
			}
		})
	}
}

// A box that clips only one axis does not hide a pop whose trigger
// overflows it on the other.
func TestTooltipFallbackClipsOnlyOnTheAxisABoxClips(t *testing.T) {
	tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft", ID: "tip"},
		render.HTML(`<button id="trigger" type="button">Save</button>`))
	ctx := tooltipBodyPage(t, fallbackKit, `<div style="overflow-x: clip; overflow-y: visible; height: 40px; width: 300px; margin: 150px 0 0 200px"><div style="padding-top: 80px">`+string(tip)+`</div></div>`, 900, 700)
	focusID(t, ctx, "trigger")
	waitOpen(t, ctx, "tip", "focus did not open the pop")
	var visible bool
	evalJS(t, ctx, `getComputedStyle(document.getElementById('tip')).visibility === 'visible'`, &visible)
	if !visible {
		t.Fatal("the pop was hidden for a trigger below a box that clips only horizontally")
	}
}

// Entering an inner tooltip's trigger straight from outside hovers the
// outer tooltip too: focus leaving the outer trigger then leaves the outer
// pop open while the pointer is still inside its wrapper.
func TestTooltipNestedHoverReachesTheOuter(t *testing.T) {
	inner := ui.Tooltip(ui.TooltipConfig{Text: "Inner help", ID: "inner", Placement: ui.TooltipBottom},
		render.HTML(`<button id="inner-trigger" type="button">Inner</button>`))
	outer := ui.Tooltip(ui.TooltipConfig{Text: "Outer help", ID: "outer"},
		render.HTML(`<span id="outer-trigger" tabindex="0" style="display: inline-block; padding: 20px; background: #eee">Outer `+string(inner)+`</span>`))
	ctx := tooltipBodyPage(t, anchoredKit, `<div style="padding: 150px 200px">`+string(outer)+`</div><button id="elsewhere" type="button" style="margin: 200px">Elsewhere</button>`, 1000, 700)
	movePointer(t, ctx, 990, 690)
	hoverID(t, ctx, "inner-trigger")
	waitOpen(t, ctx, "inner", "hover did not open the inner pop")
	focusID(t, ctx, "outer-trigger")
	waitOpen(t, ctx, "outer", "focus did not open the outer pop")
	focusID(t, ctx, "elsewhere")
	settle()
	if !tipOpen(ctx, "outer") {
		t.Fatal("the outer pop closed when focus left while the pointer stayed inside its wrapper")
	}
}
