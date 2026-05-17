package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// E2E contract tests for the 10 new primitives. Each test exercises
// the live demo page through a real headless browser and asserts the
// behavioural baseline apps depend on.

// ─── Layout ─────────────────────────────────────────────────────────

func TestE2E_Layout_StackUsesFlexColumn(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var display, direction string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/layout"),
		pageReady(),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-stack')).display`, &display),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-stack')).flexDirection`, &direction),
	); err != nil {
		t.Fatalf("layout: %v", err)
	}
	if display != "flex" {
		t.Errorf("Stack display = %q, want flex", display)
	}
	if direction != "column" {
		t.Errorf("Stack flex-direction = %q, want column", direction)
	}
}

func TestE2E_Layout_GridUsesAutoFit(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var display string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/layout"),
		pageReady(),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.ui-grid')).display`, &display),
	); err != nil {
		t.Fatalf("grid: %v", err)
	}
	if display != "grid" {
		t.Errorf("Grid display = %q, want grid", display)
	}
}

// ─── Card ───────────────────────────────────────────────────────────

func TestE2E_Card_LabelledByHeading(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var labelledBy, role string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/card"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-card"]').getAttribute('aria-labelledby')`, &labelledBy),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-card"]').getAttribute('role')`, &role),
	); err != nil {
		t.Fatalf("card: %v", err)
	}
	if !strings.HasPrefix(labelledBy, "ui-card-") {
		t.Errorf("card aria-labelledby = %q, want ui-card-*", labelledBy)
	}
	// html.Section sets role="region"
	if role != "region" {
		t.Errorf("card role = %q, want region", role)
	}
}

func TestE2E_Card_InteractiveIsAnchor(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var tag, href string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/card"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-card"]').tagName`, &tag),
		chromedp.Evaluate(`document.querySelector('a[data-fui-comp="ui-card"]').getAttribute('href')`, &href),
	); err != nil {
		t.Fatalf("card interactive: %v", err)
	}
	if tag != "A" {
		t.Errorf("interactive card tag = %q, want A", tag)
	}
	if href == "" {
		t.Errorf("interactive card must have href")
	}
}

// ─── OptimizedImage — CLS-safe ──────────────────────────────────────

func TestE2E_OptimizedImage_HasWidthHeightAndLazyLoading(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var w, h, loading, decoding string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/image"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-image__img').getAttribute('width')`, &w),
		chromedp.Evaluate(`document.querySelector('.ui-image__img').getAttribute('height')`, &h),
		chromedp.Evaluate(`document.querySelector('.ui-image__img').getAttribute('loading')`, &loading),
		chromedp.Evaluate(`document.querySelector('.ui-image__img').getAttribute('decoding')`, &decoding),
	); err != nil {
		t.Fatalf("image: %v", err)
	}
	if w == "" || h == "" {
		t.Errorf("image must have width+height for CLS: w=%q h=%q", w, h)
	}
	if loading != "lazy" {
		t.Errorf("image loading = %q, want lazy", loading)
	}
	if decoding != "async" {
		t.Errorf("image decoding = %q, want async", decoding)
	}
}

// ─── Toggle — Checkbox click flips checked ──────────────────────────

func TestE2E_Toggle_CheckboxClickTogglesChecked(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var before, after bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[name="demo-promo"]').checked`, &before),
		chromedp.Evaluate(`document.querySelector('input[name="demo-promo"]').click()`, nil),
		chromedp.Evaluate(`document.querySelector('input[name="demo-promo"]').checked`, &after),
	); err != nil {
		t.Fatalf("toggle: %v", err)
	}
	if before == after {
		t.Errorf("click did not toggle: before=%v after=%v", before, after)
	}
}

// Verifies the native <label for=…> → <input id=…> wiring: clicking
// the label text (NOT the radio circle) must still flip the input.
// This is the contract that lets a screen-reader user activate the
// control by hearing the visible label and tapping anywhere on the
// row — broken if the wrapping <label> ever loses its `for` attribute
// or the input loses its matching `id`.
func TestE2E_Toggle_RadioLabelClickTogglesInput(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var proCheckedBefore, proCheckedAfter, freeCheckedAfter bool
	var labelText string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="pro"]').checked`, &proCheckedBefore),
		// Click the LABEL'S visible text (not the radio control). The
		// .ui-toggle__label span is the rendered text "Pro — $12/mo".
		chromedp.Evaluate(`(() => {
            const pro = document.querySelector('input[name="demo-plan"][value="pro"]');
            const label = pro.closest('label');
            const text = label.querySelector('.ui-toggle__label');
            text.click();
            return text.textContent.trim();
        })()`, &labelText),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="pro"]').checked`, &proCheckedAfter),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="free"]').checked`, &freeCheckedAfter),
	); err != nil {
		t.Fatalf("radio label click: %v", err)
	}
	if proCheckedBefore {
		t.Error("Pro should start unchecked (Free is the default)")
	}
	if !proCheckedAfter {
		t.Errorf("clicking the label text %q should check the Pro radio (native for=id wiring)", labelText)
	}
	if freeCheckedAfter {
		t.Error("Free should be cleared once Pro is checked (radio mutual exclusivity)")
	}
}

// Same contract for Checkbox — clicking the label text flips the
// input. Caught a regression once when the wrapping <label> rendered
// without for=id.
func TestE2E_Toggle_CheckboxLabelClickTogglesInput(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var before, after bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[name="demo-promo"]').checked`, &before),
		chromedp.Evaluate(`document.querySelector('input[name="demo-promo"]').closest('label').querySelector('.ui-toggle__label').click()`, nil),
		chromedp.Evaluate(`document.querySelector('input[name="demo-promo"]').checked`, &after),
	); err != nil {
		t.Fatalf("checkbox label click: %v", err)
	}
	if before == after {
		t.Errorf("clicking the checkbox label text should toggle the input; before=%v after=%v", before, after)
	}
}

func TestE2E_Toggle_RadioGroupExclusive(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var teamCheckedBefore, freeCheckedAfter, teamCheckedAfter bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/toggle"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="team"]').checked`, &teamCheckedBefore),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="team"]').click()`, nil),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="free"]').checked`, &freeCheckedAfter),
		chromedp.Evaluate(`document.querySelector('input[name="demo-plan"][value="team"]').checked`, &teamCheckedAfter),
	); err != nil {
		t.Fatalf("radio: %v", err)
	}
	if teamCheckedBefore {
		t.Error("team should start unchecked")
	}
	if !teamCheckedAfter || freeCheckedAfter {
		t.Errorf("radios not mutually exclusive: free=%v team=%v", freeCheckedAfter, teamCheckedAfter)
	}
}

// ─── Tooltip — pop element wired via aria-describedby ───────────────

func TestE2E_Tooltip_TriggerHasAriaDescribedBy(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var describedBy, popRole string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tooltip"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-tooltip"] button').getAttribute('aria-describedby')`, &describedBy),
		chromedp.Evaluate(`document.querySelector('.ui-tooltip__pop').getAttribute('role')`, &popRole),
	); err != nil {
		t.Fatalf("tooltip: %v", err)
	}
	if describedBy == "" {
		t.Errorf("tooltip trigger should carry aria-describedby")
	}
	if popRole != "tooltip" {
		t.Errorf("pop role = %q, want tooltip", popRole)
	}
}

// ─── Popover — click opens, Esc closes ──────────────────────────────

// Anchored popover behaviour across viewports + side preferences.
// Each row asserts: the runtime picks a non-overflowing side,
// the originating trigger is highlighted, the arrow CSS variable is
// set, and the popover body's "from" signal reflects the trigger.
//
// We don't assert "side == requested" because the whole point of
// auto-flip is that the runtime overrides a requested side that
// would overflow. We instead assert: the chosen side is sensible
// (the popover is fully inside the viewport with 8px margin) AND
// the chosen side matches the request when there's room.
func TestE2E_Popover_AnchorPlacementParameterized(t *testing.T) {
	cases := []struct {
		viewportW, viewportH int
	}{
		{viewportW: 1440, viewportH: 900},
		{viewportW: 1024, viewportH: 768},
		{viewportW: 900, viewportH: 800},
		{viewportW: 768, viewportH: 1024}, // portrait tablet
	}
	prefs := []string{"top", "right", "bottom", "left"}

	for _, vp := range cases {
		vp := vp
		for _, pref := range prefs {
			pref := pref
			t.Run(fmtViewportCase(vp.viewportW, vp.viewportH, pref), func(t *testing.T) {
				base := startE2EServer(t)
				ctx := newE2EBrowserCtx(t)
				var info map[string]any
				if err := chromedp.Run(ctx,
					chromedp.EmulateViewport(int64(vp.viewportW), int64(vp.viewportH)),
					chromedp.Navigate(base+"/components/popover"),
					pageReady(),
					chromedp.Evaluate(`(() => {
                        const btn = document.querySelector('button[data-fui-deeplink="from=`+pref+`"]');
                        if (!btn) return {missing: true};
                        btn.click();
                        return new Promise(r => setTimeout(() => {
                            const widget = document.querySelector('[data-fui-widget="components-popover"]');
                            const wr = widget.getBoundingClientRect();
                            const vw = window.innerWidth, vh = window.innerHeight;
                            r({
                                missing: false,
                                requestedSide: btn.getAttribute('data-fui-popover-anchor'),
                                chosenSide: widget.getAttribute('data-fui-popover-side'),
                                triggerActive: btn.classList.contains('is-popover-trigger-active'),
                                fromText: document.querySelector('[data-fui-signal="from"]')?.textContent,
                                widgetX: wr.x, widgetY: wr.y,
                                widgetRight: wr.right, widgetBottom: wr.bottom,
                                vpW: vw, vpH: vh,
                            });
                        }, 250));
                    })()`, &info,
						func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
				); err != nil {
					t.Fatalf("chromedp: %v", err)
				}
				if info["missing"] == true {
					t.Fatalf("trigger for pref %q missing on /components/popover", pref)
				}
				if info["triggerActive"] != true {
					t.Errorf("originating trigger should carry is-popover-trigger-active; got %v", info["triggerActive"])
				}
				if got, _ := info["fromText"].(string); got != pref {
					t.Errorf("popover body 'from' signal = %q, want %q (so user can tell which trigger fired)", got, pref)
				}
				chosen, _ := info["chosenSide"].(string)
				switch chosen {
				case "top", "right", "bottom", "left":
				default:
					t.Errorf("chosenSide must be one of top/right/bottom/left; got %q", chosen)
				}
				// No overflow — every edge of the widget rect must
				// fit inside the viewport (with a small slack for
				// box-shadow rendering).
				x := toFloat(info["widgetX"])
				y := toFloat(info["widgetY"])
				right := toFloat(info["widgetRight"])
				bottom := toFloat(info["widgetBottom"])
				vw := toFloat(info["vpW"])
				vh := toFloat(info["vpH"])
				if x < 0 || y < 0 {
					t.Errorf("popover should sit inside viewport (x>=0, y>=0); got x=%v y=%v", x, y)
				}
				if right > vw+1 || bottom > vh+1 {
					t.Errorf("popover overflows viewport: right=%v bottom=%v vp=%vx%v", right, bottom, vw, vh)
				}
			})
		}
	}
}

func fmtViewportCase(w, h int, pref string) string {
	return strconv.Itoa(w) + "x" + strconv.Itoa(h) + "/" + pref
}

func toFloat(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// When the page scrolls, an anchored popover must track its trigger
// (the runtime listens to scroll events and re-runs place()).
// Without this the popover stays glued to the viewport while the
// trigger moves underneath it.
func TestE2E_Popover_FollowsTriggerOnScroll(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base+"/components/popover"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const btn = document.querySelector('button[data-fui-deeplink="from=top"]');
            btn.click();
            return new Promise(r => setTimeout(() => {
                const widget = document.querySelector('[data-fui-widget="components-popover"]');
                const beforeTr = btn.getBoundingClientRect();
                const beforeW  = widget.getBoundingClientRect();
                // Scroll the page; trigger should move, popover should
                // follow within ~1 rAF.
                window.scrollBy(0, 200);
                requestAnimationFrame(() => requestAnimationFrame(() => {
                    const afterTr = btn.getBoundingClientRect();
                    const afterW  = widget.getBoundingClientRect();
                    r({
                        triggerMovedBy: beforeTr.y - afterTr.y,
                        widgetMovedBy: beforeW.y - afterW.y,
                        // Distance between trigger and widget (the
                        // popover anchors with a small gap). After
                        // tracking, this distance should stay roughly
                        // unchanged.
                        gapBefore: beforeTr.y - beforeW.bottom,
                        gapAfter: afterTr.y - afterW.bottom,
                    });
                }));
            }, 250));
        })()`, &info,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	tMoved := toFloat(info["triggerMovedBy"])
	wMoved := toFloat(info["widgetMovedBy"])
	if tMoved < 100 {
		t.Fatalf("trigger should have moved ~200px on scroll; got %v", tMoved)
	}
	// Popover should have moved by the same amount (±5px slack for
	// sub-pixel rounding and rAF timing).
	diff := tMoved - wMoved
	if diff < -5 || diff > 5 {
		t.Errorf("popover should track the trigger on scroll: trigger moved %.1f, popover moved %.1f (diff %.1f)", tMoved, wMoved, diff)
	}
	gapBefore := toFloat(info["gapBefore"])
	gapAfter := toFloat(info["gapAfter"])
	gapDiff := gapBefore - gapAfter
	if gapDiff < -5 || gapDiff > 5 {
		t.Errorf("gap between popover and trigger should stay roughly constant after scroll: before=%.1f after=%.1f", gapBefore, gapAfter)
	}
}

// Bottom-corner trigger in the edge demo MUST flip the popover up
// because BOTTOM placement would push past the viewport. This was
// the original "they all open downwards" complaint.
func TestE2E_Popover_BottomCornerTriggerFlipsUp(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base+"/components/popover"),
		pageReady(),
		// Scroll the edge frame's bottom-right trigger so it sits near
		// the viewport's lower edge — block: 'end' aligns the trigger
		// to the bottom of the viewport, which is the natural viewing
		// position for a bottom-corner button. Auto-anchor MUST flip
		// the popover up from this position.
		chromedp.Evaluate(`document.querySelector('button[data-fui-deeplink="from=edge-br"]').scrollIntoView({block: 'end'})`, nil),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const btn = document.querySelector('button[data-fui-deeplink="from=edge-br"]');
            btn.click();
            return new Promise(r => setTimeout(() => {
                const w = document.querySelector('[data-fui-widget="components-popover"]');
                const tr = btn.getBoundingClientRect();
                const wr = w.getBoundingClientRect();
                r({
                    side: w.getAttribute('data-fui-popover-side'),
                    triggerY: tr.y, triggerBottom: tr.bottom,
                    widgetY: wr.y, widgetBottom: wr.bottom,
                    vh: window.innerHeight,
                });
            }, 250));
        })()`, &info,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	side, _ := info["side"].(string)
	// A bottom-corner trigger should NOT pick "bottom" — that would
	// overflow the viewport (the whole point of auto-flip).
	if side == "bottom" {
		t.Errorf("bottom-corner trigger should auto-flip away from bottom (got %q). triggerBottom=%v vh=%v widgetBottom=%v",
			side, info["triggerBottom"], info["vh"], info["widgetBottom"])
	}
	// Popover must still fit inside the viewport.
	if toFloat(info["widgetBottom"]) > toFloat(info["vh"])+1 {
		t.Errorf("popover overflows viewport: widgetBottom=%v vh=%v", info["widgetBottom"], info["vh"])
	}
}

// At a viewport too short to fit the popover body, the chrome's
// max-block-size + overflow-y: auto must kick in so the body
// scrolls inside the popover rather than overflowing the page.
func TestE2E_Popover_ScrollsWhenTallerThanViewport(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	if err := chromedp.Run(ctx,
		// 320px-tall viewport is shorter than the demo body (~220px)
		// minus the 32px viewport margin minus the trigger row.
		// Anchored popover should be capped to vh-32 = 288px and the
		// body must scroll.
		chromedp.EmulateViewport(1024, 320),
		chromedp.Navigate(base+"/components/popover"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const btn = document.querySelector('button[data-fui-deeplink="from=bottom"]');
            btn.click();
            return new Promise(r => setTimeout(() => {
                const widget = document.querySelector('[data-fui-widget="components-popover"]');
                // Scroll lives on the inner .fui-slot (the root keeps
                // overflow:visible so the arrow stays visible). Force
                // the slot to overflow to verify it scrolls.
                const slot = widget.querySelector('.fui-slot');
                const stuffer = document.createElement('div');
                stuffer.style.height = '600px';
                stuffer.textContent = 'tall content';
                slot.appendChild(stuffer);
                const wr = widget.getBoundingClientRect();
                const wcs = getComputedStyle(widget);
                const scs = getComputedStyle(slot);
                r({
                    widgetH: wr.height,
                    vpH: window.innerHeight,
                    rootOverflowY: wcs.overflowY,
                    slotOverflowY: scs.overflowY,
                    slotScrollHeight: slot.scrollHeight,
                    slotClientHeight: slot.clientHeight,
                });
            }, 300));
        })()`, &info,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	wh := toFloat(info["widgetH"])
	vh := toFloat(info["vpH"])
	if wh > vh {
		t.Errorf("popover height %v should be ≤ viewport %v (max-block-size cap)", wh, vh)
	}
	// Root must stay visible so the arrow ::before renders.
	rootOverflow, _ := info["rootOverflowY"].(string)
	if rootOverflow == "auto" || rootOverflow == "scroll" || rootOverflow == "hidden" {
		t.Errorf("popover root overflow-y=%q would clip the arrow ::before; keep it visible", rootOverflow)
	}
	// Inner slot must scroll on tall content.
	slotOverflow, _ := info["slotOverflowY"].(string)
	if slotOverflow != "auto" && slotOverflow != "scroll" {
		t.Errorf("popover .fui-slot overflow-y=%q, want auto or scroll", slotOverflow)
	}
	if toFloat(info["slotScrollHeight"]) <= toFloat(info["slotClientHeight"]) {
		t.Errorf("with the 600px stuffer, slot scrollHeight should exceed clientHeight (content must overflow + scroll)")
	}
}

// Re-opening the popover from a different trigger should clear the
// previous trigger's active state and apply it to the new one.
func TestE2E_Popover_TriggerActiveClassFollowsCurrentTrigger(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var info map[string]any
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 800),
		chromedp.Navigate(base+"/components/popover"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const t1 = document.querySelector('button[data-fui-deeplink="from=top"]');
            const t2 = document.querySelector('button[data-fui-deeplink="from=bottom"]');
            t1.click();
            return new Promise(r => setTimeout(() => {
                const t1Active1 = t1.classList.contains('is-popover-trigger-active');
                window.__gofastr.closeWidget('components-popover');
                t2.click();
                setTimeout(() => r({
                    t1ActiveWhileOpenedFromT1: t1Active1,
                    t1ActiveAfterReopenFromT2: t1.classList.contains('is-popover-trigger-active'),
                    t2ActiveAfterReopenFromT2: t2.classList.contains('is-popover-trigger-active'),
                }), 250);
            }, 250));
        })()`, &info,
			func(p *runtime.EvaluateParams) *runtime.EvaluateParams { return p.WithAwaitPromise(true) }),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if info["t1ActiveWhileOpenedFromT1"] != true {
		t.Errorf("trigger 1 should be active while its popover is open")
	}
	if info["t1ActiveAfterReopenFromT2"] != false {
		t.Errorf("trigger 1 should NOT carry active class after popover re-opens from trigger 2")
	}
	if info["t2ActiveAfterReopenFromT2"] != true {
		t.Errorf("trigger 2 should carry active class once popover opens from it")
	}
}

func TestE2E_Popover_OpensOnClickAndDismissesOnEscape(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var openedAfterClick, dismissed bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/popover"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('button[data-fui-open="components-popover"]').click()`, nil),
		// Hidden + lazy-fetched chrome needs a moment to land.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`(() => {
            const el = document.querySelector('[data-fui-widget="components-popover"]');
            return !!el && !el.hasAttribute('hidden') && getComputedStyle(el).display !== 'none';
        })()`, &openedAfterClick),
		chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}))`, nil),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(() => {
            const el = document.querySelector('[data-fui-widget="components-popover"]');
            return !el || el.hasAttribute('hidden') || getComputedStyle(el).display === 'none';
        })()`, &dismissed),
	); err != nil {
		t.Fatalf("popover: %v", err)
	}
	if !openedAfterClick {
		t.Error("popover should be open after click")
	}
	if !dismissed {
		t.Error("popover should dismiss on Escape")
	}
}

// ─── Tag — dismiss × is a button with aria-label ────────────────────

func TestE2E_Tag_DismissButtonHasAccessibleLabel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var ariaLabel, rpcPath string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tag"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('.ui-tag__dismiss').getAttribute('aria-label')`, &ariaLabel),
		chromedp.Evaluate(`document.querySelector('.ui-tag__dismiss').getAttribute('data-fui-rpc')`, &rpcPath),
	); err != nil {
		t.Fatalf("tag: %v", err)
	}
	if !strings.HasPrefix(ariaLabel, "Remove ") {
		t.Errorf("dismiss aria-label = %q, want 'Remove …'", ariaLabel)
	}
	if rpcPath == "" {
		t.Errorf("dismiss button should carry data-fui-rpc")
	}
}

// ─── Spinner — announces loading once ───────────────────────────────

func TestE2E_Spinner_HasStatusRoleAndAriaBusy(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var role, ariaBusy, hiddenLabel string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/spinner"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-spinner"]').getAttribute('role')`, &role),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-spinner"]').getAttribute('aria-busy')`, &ariaBusy),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-spinner"] .ui-visually-hidden').textContent`, &hiddenLabel),
	); err != nil {
		t.Fatalf("spinner: %v", err)
	}
	if role != "status" {
		t.Errorf("role = %q, want status", role)
	}
	if ariaBusy != "true" {
		t.Errorf("aria-busy = %q, want true", ariaBusy)
	}
	if !strings.Contains(hiddenLabel, "Loading") {
		t.Errorf("expected screen-reader label, got %q", hiddenLabel)
	}
}

// ─── Divider — plain renders as <hr> ────────────────────────────────

func TestE2E_Divider_PlainUsesHR(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var tag, labelRole string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/divider"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('hr[data-fui-comp="ui-divider"]').tagName`, &tag),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-divider"].ui-divider--labelled').getAttribute('role')`, &labelRole),
	); err != nil {
		t.Fatalf("divider: %v", err)
	}
	if tag != "HR" {
		t.Errorf("plain divider should render <hr>, got %q", tag)
	}
	if labelRole != "separator" {
		t.Errorf("labelled divider role = %q, want separator", labelRole)
	}
}

// Setting input.files via the DataTransfer API to simulate a real
// drag-drop, then asserting the runtime renders the filename list +
// thumbnail and the form's POST flow round-trips the names back.
func TestE2E_FileUpload_FullFlow(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var preview string
	var resultHTML string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/fileupload"),
		pageReady(),
		// Simulate drop: assign a File to the input + dispatch change.
		// Native `new File()` works in headless Chrome.
		chromedp.Evaluate(`(() => {
            const input = document.querySelector('input[name="files"]');
            const dt = new DataTransfer();
            dt.items.add(new File(["hello world"], "notes.txt", {type: "text/plain"}));
            input.files = dt.files;
            input.dispatchEvent(new Event('change', {bubbles: true}));
            return document.querySelector('[data-fui-comp="ui-fileupload"] .ui-fileupload__filename').innerText;
        })()`, &preview),
		// Submit the form via the standard data-fui-rpc path.
		chromedp.Evaluate(`document.querySelector('form[data-fui-rpc]').requestSubmit()`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('.demo-upload-result').innerHTML`, &resultHTML),
	); err != nil {
		t.Fatalf("fileupload full flow: %v", err)
	}
	if !strings.Contains(preview, "notes.txt") {
		t.Errorf("filename preview should contain notes.txt; got %q", preview)
	}
	if !strings.Contains(resultHTML, "notes.txt") {
		t.Errorf("server echo island should contain notes.txt; got %q", resultHTML)
	}
}

// ─── FileUpload — drop zone wired, input type=file ──────────────────

func TestE2E_FileUpload_NativeInputAndDropZone(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var inputType, accept string
	var hasDropZone bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/fileupload"),
		pageReady(),
		chromedp.Evaluate(`document.querySelector('input[name="demo-doc"]').getAttribute('type')`, &inputType),
		chromedp.Evaluate(`document.querySelector('input[name="demo-doc"]').getAttribute('accept')`, &accept),
		chromedp.Evaluate(`!!document.querySelector('[data-fui-fileupload]')`, &hasDropZone),
	); err != nil {
		t.Fatalf("fileupload: %v", err)
	}
	if inputType != "file" {
		t.Errorf("input type = %q, want file", inputType)
	}
	if accept == "" {
		t.Errorf("expected accept attribute to be passed through")
	}
	if !hasDropZone {
		t.Errorf("expected data-fui-fileupload drop zone marker")
	}
}

// ─── Index page lists all 10 primitives ─────────────────────────────

func TestE2E_ComponentsIndex_ListsAllPrimitives(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	primitives := []string{"layout", "card", "image", "toggle", "tooltip", "popover", "tag", "spinner", "divider", "fileupload"}
	for _, slug := range primitives {
		var exists bool
		if err := chromedp.Run(ctx,
			chromedp.Navigate(base+"/components/"),
			pageReady(),
			chromedp.Evaluate(`!!document.querySelector('a[href="/components/`+slug+`"]')`, &exists),
		); err != nil {
			t.Fatalf("index: %v", err)
		}
		if !exists {
			t.Errorf("components index missing entry for %q", slug)
		}
	}
}
