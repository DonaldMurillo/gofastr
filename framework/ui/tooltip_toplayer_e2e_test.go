package ui_test

// The tooltip on the top layer (#706): the headless-tooltip module raises
// the pop into the top layer, where a clipping ancestor cannot hide it;
// the pop can be hovered, is dismissed by Escape without moving the
// pointer or focus (WCAG 1.4.13), and sits beside its trigger whether
// the browser places it by anchor positioning or the module does.

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// tooltipPage serves a tooltip on a button inside a short box that clips
// its overflow, at the top of the viewport, so the preferred top side has
// no room; a second tooltip sits inside a dialog.
func tooltipPage(t *testing.T, anchorPositioning bool, pre ...chromedp.Action) context.Context {
	t.Helper()
	url := tooltipServer(t, anchorPositioning, nil)
	return moduleTestCtxURL(t, url, append(pre, emulateFocus(), chromedp.EmulateViewport(900, 600))...)
}

// A headless page has no focus of its own, so the browser fires no
// focus events until focus is emulated.
func emulateFocus() chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return emulation.SetFocusEmulationEnabled(true).Do(ctx)
	})
}

// tooltipServer serves the page tooltipPage opens and returns its URL.
func tooltipServer(t *testing.T, anchorPositioning bool, extra func(mux *http.ServeMux)) string {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	th := theme.Default()
	var css strings.Builder
	for _, e := range registry.All() {
		css.WriteString(e.CSSFor(th))
		css.WriteString("\n")
	}
	sheet := css.String()
	if !anchorPositioning {
		sheet = withoutBlock(t, sheet, "@supports (anchor-scope: --x) and (position-area: top) {")
	}
	head := "<style>" + th.CSSCustomProperties() + "\n" + sheet + "</style>"
	tip := ui.Tooltip(ui.TooltipConfig{Text: "Saves the draft without closing it", ID: "tip"},
		render.HTML(`<button id="trigger" type="button">Save</button>`))
	inDialog := ui.Tooltip(ui.TooltipConfig{Text: "Inside the dialog", ID: "tip-dialog"},
		render.HTML(`<button id="dialog-trigger" type="button">Help</button>`))
	body := `<div id="clip" style="overflow: hidden; width: 220px; height: 36px; margin: 4px 120px">` + string(tip) + `</div>` +
		`<button id="elsewhere" type="button" style="margin: 300px 0 0 20px">Elsewhere</button>` +
		`<dialog id="dlg"><p>A dialog</p>` + string(inDialog) + `</dialog>`
	var adds []func(mux *http.ServeMux)
	if extra != nil {
		adds = append(adds, extra)
	}
	return themeTestPageWithHead(t, head, body, adds...).URL
}

// withoutBlock drops the brace-balanced block that opens with head from
// css, as a browser that does not support it ignores it.
func withoutBlock(t *testing.T, css, head string) string {
	t.Helper()
	at := strings.Index(css, head)
	if at < 0 {
		t.Fatalf("the sheet has no %q block", head)
	}
	depth := 0
	for i := at; i < len(css); i++ {
		switch css[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return css[:at] + css[i+1:]
			}
		}
	}
	t.Fatalf("the %q block never closes", head)
	return css
}

type tipRect struct {
	Top, Bottom, Left, Right float64
}

func rectOf(t *testing.T, ctx context.Context, id string) tipRect {
	t.Helper()
	var r tipRect
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const r = document.getElementById('`+id+`').getBoundingClientRect(); return {Top: r.top, Bottom: r.bottom, Left: r.left, Right: r.right}; })()`, &r)); err != nil {
		t.Fatal(err)
	}
	return r
}

func movePointer(t *testing.T, ctx context.Context, x, y float64) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		return input.DispatchMouseEvent(input.MouseMoved, x, y).Do(ctx)
	})); err != nil {
		t.Fatal(err)
	}
}

func hoverID(t *testing.T, ctx context.Context, id string) {
	t.Helper()
	r := rectOf(t, ctx, id)
	movePointer(t, ctx, (r.Left+r.Right)/2, (r.Top+r.Bottom)/2)
}

func tipOpen(ctx context.Context, id string) bool {
	var open bool
	_ = chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('`+id+`').matches(':popover-open')`, &open))
	return open
}

func pressEscape(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.KeyEvent("\x1b")); err != nil {
		t.Fatal(err)
	}
}

// settle waits out the module's hide delay.
func settle() { time.Sleep(400 * time.Millisecond) }

func TestTooltipRisesIntoTheTopLayerOutOfAClippingBox(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hovering the trigger did not open the pop as a popover")
	}
	pop, trigger, clip := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger"), rectOf(t, ctx, "clip")
	if pop.Top < trigger.Bottom-1 {
		t.Errorf("the pop's top %v is above the trigger's bottom %v: with no room above it should flip below", pop.Top, trigger.Bottom)
	}
	if mid := (pop.Left + pop.Right) / 2; mid < trigger.Left || mid > trigger.Right {
		t.Errorf("pop centre %v is not over the trigger (%v..%v)", mid, trigger.Left, trigger.Right)
	}
	if pop.Bottom <= clip.Bottom {
		t.Fatalf("the pop (bottom %v) does not reach past the clipping box (bottom %v), so the test proves nothing", pop.Bottom, clip.Bottom)
	}
	var hit string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const r = document.getElementById('tip').getBoundingClientRect(); const el = document.elementFromPoint((r.left + r.right) / 2, r.bottom - 2); return el ? el.id : ''; })()`, &hit)); err != nil {
		t.Fatal(err)
	}
	if hit != "tip" {
		t.Fatalf("the part of the pop past the clipping box is not drawn (hit %q): it is not in the top layer", hit)
	}
}

func TestTooltipCanBeHoveredAndClosesWhenThePointerLeaves(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hover did not open the pop")
	}
	// Across the gap and onto the pop: it stays.
	trigger, pop := rectOf(t, ctx, "trigger"), rectOf(t, ctx, "tip")
	movePointer(t, ctx, (trigger.Left+trigger.Right)/2, (trigger.Bottom+pop.Top)/2)
	hoverID(t, ctx, "tip")
	settle()
	if !tipOpen(ctx, "tip") {
		t.Fatal("the pop closed while the pointer was over it")
	}
	movePointer(t, ctx, 880, 580)
	settle()
	if tipOpen(ctx, "tip") {
		t.Fatal("the pop stayed open after the pointer left the trigger and the pop")
	}
}

func TestTooltipEscapeDismissesWithoutMovingThePointer(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hover did not open the pop")
	}
	pressEscape(t, ctx)
	settle()
	if tipOpen(ctx, "tip") {
		t.Fatal("Escape did not close the pop")
	}
	// The pointer moving within the trigger does not bring it back.
	r := rectOf(t, ctx, "trigger")
	movePointer(t, ctx, (r.Left+r.Right)/2+3, (r.Top+r.Bottom)/2+1)
	settle()
	if tipOpen(ctx, "tip") {
		t.Fatal("the dismissed pop came back while the pointer stayed on the trigger")
	}
	var shown bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`getComputedStyle(document.getElementById('tip')).display !== 'none'`, &shown)); err != nil {
		t.Fatal(err)
	}
	if shown {
		t.Fatal("the dismissed pop is still drawn while the pointer rests on the trigger")
	}
	// Leaving and coming back shows it again.
	movePointer(t, ctx, 880, 580)
	settle()
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("the pop did not open again after the pointer left and came back")
	}
}

func TestTooltipFollowsFocus(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Focus(`#trigger`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("focusing the trigger did not open the pop")
	}
	if err := chromedp.Run(ctx, chromedp.Focus(`#elsewhere`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	settle()
	if tipOpen(ctx, "tip") {
		t.Fatal("moving focus away did not close the pop")
	}
}

// Escape closes the tooltip first: the dialog it sits in stays open
// until the next Escape.
func TestTooltipEscapeLeavesTheDialogOpen(t *testing.T) {
	ctx := tooltipPage(t, true)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('dlg').showModal()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Focus(`#dialog-trigger`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.getElementById('tip-dialog').matches(':popover-open')`) {
		t.Fatal("focus inside the dialog did not open its tooltip")
	}
	pressEscape(t, ctx)
	settle()
	var dialogOpen bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('dlg').open`, &dialogOpen)); err != nil {
		t.Fatal(err)
	}
	if tipOpen(ctx, "tip-dialog") || !dialogOpen {
		t.Fatalf("after one Escape: tooltip open %v, dialog open %v; want closed and open", tipOpen(ctx, "tip-dialog"), dialogOpen)
	}
	pressEscape(t, ctx)
	settle()
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('dlg').open`, &dialogOpen)); err != nil {
		t.Fatal(err)
	}
	if dialogOpen {
		t.Fatal("the second Escape did not reach the dialog")
	}
}

// A browser without anchor positioning: the module places the pop, on
// the opposite side when the preferred one has no room.
func TestTooltipPlacesItselfWithoutAnchorPositioning(t *testing.T) {
	noAnchor := chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := page.AddScriptToEvaluateOnNewDocument(`(() => { const s = CSS.supports.bind(CSS); CSS.supports = (...a) => /anchor|position-area/.test(a.join(':')) ? false : s(...a); })()`).Do(ctx)
		return err
	})
	ctx := tooltipPage(t, false, noAnchor)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hover did not open the pop")
	}
	var placed string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('tip').style.top`, &placed)); err != nil {
		t.Fatal(err)
	}
	if placed == "" {
		t.Fatal("the module did not place the pop when anchor positioning is absent")
	}
	pop, trigger := rectOf(t, ctx, "tip"), rectOf(t, ctx, "trigger")
	if pop.Top < trigger.Bottom-1 || pop.Top > trigger.Bottom+24 {
		t.Fatalf("pop top %v, trigger bottom %v: want the pop just below the trigger", pop.Top, trigger.Bottom)
	}
	if mid := (pop.Left + pop.Right) / 2; mid < trigger.Left || mid > trigger.Right {
		t.Fatalf("pop centre %v is not over the trigger (%v..%v)", mid, trigger.Left, trigger.Right)
	}
}

// Escape that closes a tooltip goes no further: a page-level Escape
// handler (an overlay's close, a drawer's) does not also see it.
func TestTooltipEscapeIsConsumed(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	// One listener bubbles; the other captures on document, the target
	// the module listens on, registered after it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__escSeen = 0; document.addEventListener('keydown', e => { if (e.key === 'Escape') window.__escSeen++; }); document.addEventListener('keydown', e => { if (e.key === 'Escape') window.__escSeen++; }, true)`, nil)); err != nil {
		t.Fatal(err)
	}
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hover did not open the pop")
	}
	pressEscape(t, ctx)
	settle()
	var seen int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__escSeen`, &seen)); err != nil {
		t.Fatal(err)
	}
	if seen != 0 {
		t.Fatalf("the page saw %d Escape presses that closed a tooltip", seen)
	}
	pressEscape(t, ctx)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__escSeen`, &seen)); err != nil {
		t.Fatal(err)
	}
	if seen != 2 {
		t.Fatalf("with no tooltip open, the page's two listeners saw %d Escape presses, want 2", seen)
	}
}

// Focus keeps the pop open when the pointer leaves, and a pop dismissed
// by Escape stays closed while focus is still on its trigger, even when
// the pointer comes back over it.
func TestTooltipFocusAndDismissalOutlastThePointer(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	if err := chromedp.Run(ctx, chromedp.Focus(`#trigger`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hover and focus did not open the pop")
	}
	movePointer(t, ctx, 880, 580)
	settle()
	if !tipOpen(ctx, "tip") {
		t.Fatal("the pop closed when the pointer left while focus stayed on the trigger")
	}
	pressEscape(t, ctx)
	settle()
	hoverID(t, ctx, "trigger")
	settle()
	if tipOpen(ctx, "tip") {
		t.Fatal("the pop dismissed by Escape came back on hover while focus stayed on the trigger")
	}
}

// gateModule holds the headless-tooltip module back until release is
// closed, and closes requested when the browser asks for it.
func gateModule(t *testing.T, requested, release chan struct{}) func(mux *http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("/__gofastr/runtime/headless-tooltip.js", func(w http.ResponseWriter, r *http.Request) {
			close(requested)
			select {
			case <-release:
			case <-r.Context().Done():
				return
			case <-time.After(15 * time.Second):
			}
			src, ok := runtime.Module("headless-tooltip")
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/javascript")
			w.Write([]byte(src))
		})
	}
}

// A trigger that has focus, the pointer or both before headless-tooltip
// has loaded opens on the top layer once it does, without a second focus
// or hover; with both, it stays open when focus leaves and the pointer
// stays.
func TestTooltipOpensForATriggerUsedBeforeTheModuleLoads(t *testing.T) {
	for _, how := range []string{"focus", "hover", "focus and hover"} {
		t.Run(how, func(t *testing.T) {
			requested, release := make(chan struct{}), make(chan struct{})
			url := tooltipServer(t, true, gateModule(t, requested, release))
			// The held module holds back the load event, so navigate
			// without waiting for it.
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, emulateFocus(), chromedp.EmulateViewport(900, 600),
				chromedp.ActionFunc(func(ctx context.Context) error {
					_, _, _, _, err := page.Navigate(url).Do(ctx)
					return err
				}),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			select {
			case <-requested:
			case <-time.After(10 * time.Second):
				t.Fatal("the page never asked for headless-tooltip")
			}
			if how != "focus" {
				hoverID(t, ctx, "trigger")
			}
			if how != "hover" {
				if err := chromedp.Run(ctx, chromedp.Focus(`#trigger`, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
			}
			if tipOpen(ctx, "tip") {
				t.Fatal("the pop is a popover before the module loaded, so the test proves nothing")
			}
			close(release)
			if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
				t.Fatal("headless-tooltip did not load")
			}
			if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
				t.Fatalf("the trigger that had %s before the module loaded did not open on the top layer", how)
			}
			if how == "focus and hover" {
				if err := chromedp.Run(ctx, chromedp.Focus(`#elsewhere`, chromedp.ByQuery)); err != nil {
					t.Fatal(err)
				}
				settle()
				if !tipOpen(ctx, "tip") {
					t.Fatal("the pop closed when focus left while the pointer stayed on the trigger")
				}
			}
		})
	}
}

// A tooltip whose wrapper a swap has removed does not swallow the next
// Escape: the page sees it.
func TestTooltipEscapePassesWhenItsTooltipIsGone(t *testing.T) {
	ctx := tooltipPage(t, true)
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	hoverID(t, ctx, "trigger")
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("hover did not open the pop")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__escSeen = 0; document.addEventListener('keydown', e => { if (e.key === 'Escape') window.__escSeen++; }); document.getElementById('clip').replaceChildren()`, nil)); err != nil {
		t.Fatal(err)
	}
	pressEscape(t, ctx)
	var seen int
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__escSeen`, &seen)); err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatalf("after the open tooltip was removed, the page saw %d Escape presses, want 1", seen)
	}
}

// Moving the pointer from an open tooltip onto one dismissed by Escape
// still closes the first: the dismissed one does not cancel its hide.
func TestTooltipDismissedOneLetsTheOpenOneClose(t *testing.T) {
	ctx := tooltipPage(t, true)
	// A second tooltip, cloned before either is upgraded.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const c = document.querySelector('#clip [data-hui-tooltip]').cloneNode(true);
		c.querySelector('#trigger').id = 'trigger2';
		c.querySelector('#tip').id = 'tip2';
		const box = document.createElement('div');
		box.style.cssText = 'position: fixed; left: 420px; top: 220px';
		box.append(c);
		document.body.append(box);
	})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, moduleLoaded("headless-tooltip")) {
		t.Fatal("headless-tooltip did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Focus(`#trigger`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	if !pollJS(ctx, `document.getElementById('tip').matches(':popover-open')`) {
		t.Fatal("focus did not open the first pop")
	}
	pressEscape(t, ctx)
	hoverID(t, ctx, "trigger2")
	if !pollJS(ctx, `document.getElementById('tip2').matches(':popover-open')`) {
		t.Fatal("hover did not open the second pop")
	}
	// Straight from the second trigger onto the dismissed, still focused one.
	hoverID(t, ctx, "trigger")
	settle()
	if tipOpen(ctx, "tip2") {
		t.Fatal("the second pop stayed open after the pointer left it for a dismissed tooltip")
	}
	if tipOpen(ctx, "tip") {
		t.Fatal("the dismissed pop came back")
	}
}
