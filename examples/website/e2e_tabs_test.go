package main

import (
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Tabs — single-open invariant + chaos
// =============================================================================

func TestE2E_Tabs_FirstTabDefaultsOpen(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var openCount int
	var firstLabel string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.tabs > details[open]').length`, &openCount),
		chromedp.Evaluate(`document.querySelector('.tabs > details[open] .tabs-summary').textContent`, &firstLabel),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openCount != 1 {
		t.Errorf("expected 1 tab open initially, got %d", openCount)
	}
	if !strings.Contains(strings.ToLower(firstLabel), "overview") {
		t.Errorf("expected first tab open with Overview label, got %q", firstLabel)
	}
}

func TestE2E_Tabs_ClickingSwitchesPanel(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var openLabelAfter string
	var visiblePanelText string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.tabs > details > summary')[1].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelector('.tabs > details[open] .tabs-summary').textContent`, &openLabelAfter),
		// Panels live in a sibling .tabs-panels container; the visible
		// one is the panel whose nth-of-type matches the open details.
		chromedp.Evaluate(`(() => {
            const panels = document.querySelectorAll('.tabs > .tabs-panels > .tabs-panel');
            for (const p of panels) {
                if (getComputedStyle(p).display !== 'none') return p.textContent;
            }
            return '';
        })()`, &visiblePanelText),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(strings.ToLower(openLabelAfter), "accessibility") {
		t.Errorf("expected Accessibility tab open after click, got %q", openLabelAfter)
	}
	if !strings.Contains(visiblePanelText, "keyboard") {
		t.Errorf("expected Accessibility panel content visible, got %q", visiblePanelText)
	}
}

// TestE2E_Tabs_PanelSitsBelowSummariesAndStaysWithinFrame verifies the
// spatial relationship the user actually sees: every summary is in the
// top horizontal strip, the active panel is positioned below all
// summaries, and nothing overflows the demo frame's width.
func TestE2E_Tabs_PanelSitsBelowSummariesAndStaysWithinFrame(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	type rect struct {
		Top    float64 `json:"top"`
		Bottom float64 `json:"bottom"`
		Left   float64 `json:"left"`
		Right  float64 `json:"right"`
	}
	var summaries []rect
	var panel rect
	var frameRight float64

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.tabs > details > .tabs-summary')).map(el => {
            const r = el.getBoundingClientRect();
            return {top: r.top, bottom: r.bottom, left: r.left, right: r.right};
        })`, &summaries),
		chromedp.Evaluate(`(() => {
            const panels = document.querySelectorAll('.tabs > .tabs-panels > .tabs-panel');
            for (const p of panels) {
                if (getComputedStyle(p).display !== 'none') {
                    const r = p.getBoundingClientRect();
                    return {top: r.top, bottom: r.bottom, left: r.left, right: r.right};
                }
            }
            return null;
        })()`, &panel),
		chromedp.Evaluate(`document.querySelector('.demo-live').getBoundingClientRect().right`, &frameRight),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(summaries) == 0 {
		t.Fatal("no .tabs-summary elements measured")
	}

	// (1) All summaries share the same top (within 1px) — they're in
	// the top strip together.
	topRef := summaries[0].Top
	for i, s := range summaries {
		if abs(s.Top-topRef) > 1.5 {
			t.Errorf("summary[%d].top=%.1f differs from summary[0].top=%.1f — strip not aligned",
				i, s.Top, topRef)
		}
	}

	// (2) Panel.top >= max(summaries.bottom) — panel sits below.
	maxBottom := summaries[0].Bottom
	for _, s := range summaries {
		if s.Bottom > maxBottom {
			maxBottom = s.Bottom
		}
	}
	if panel.Top < maxBottom-1 { // 1px tolerance for sub-pixel rounding
		t.Errorf("panel.top=%.1f sits ABOVE or overlaps summaries (maxBottom=%.1f)",
			panel.Top, maxBottom)
	}

	// (3) No summary or panel extends past the demo-live frame's right
	// edge — catches the previous grid-overflow clipping bug.
	for i, s := range summaries {
		if s.Right > frameRight+1 {
			t.Errorf("summary[%d].right=%.1f overflows demo-live.right=%.1f",
				i, s.Right, frameRight)
		}
	}
	if panel.Right > frameRight+1 {
		t.Errorf("panel.right=%.1f overflows demo-live.right=%.1f", panel.Right, frameRight)
	}
}

// TestE2E_Tabs_ActiveTabHasUnderline verifies the active state is
// visually distinguishable — the underline must actually paint.
func TestE2E_Tabs_ActiveTabHasUnderline(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var active, inactive struct {
		BorderBottomColor string  `json:"borderBottomColor"`
		BorderBottomWidth string  `json:"borderBottomWidth"`
		Color             string  `json:"color"`
	}

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const open = document.querySelector('.tabs > details[open] > .tabs-summary');
            const cs = getComputedStyle(open);
            return {
                borderBottomColor: cs.borderBottomColor,
                borderBottomWidth: cs.borderBottomWidth,
                color: cs.color,
            };
        })()`, &active),
		chromedp.Evaluate(`(() => {
            const closed = document.querySelector('.tabs > details:not([open]) > .tabs-summary');
            const cs = getComputedStyle(closed);
            return {
                borderBottomColor: cs.borderBottomColor,
                borderBottomWidth: cs.borderBottomWidth,
                color: cs.color,
            };
        })()`, &inactive),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// The underline must have a non-transparent color on the active tab.
	if strings.Contains(active.BorderBottomColor, "rgba(0, 0, 0, 0)") {
		t.Errorf("active tab has transparent border-bottom-color: %q", active.BorderBottomColor)
	}
	// Active and inactive must visibly differ in either color or border.
	if active.Color == inactive.Color && active.BorderBottomColor == inactive.BorderBottomColor {
		t.Errorf("active vs inactive look identical: color=%q border=%q",
			active.Color, active.BorderBottomColor)
	}
	// Underline width must be >= 1px (the border was set to 2px).
	if active.BorderBottomWidth == "" || strings.HasPrefix(active.BorderBottomWidth, "0") {
		t.Errorf("active tab has zero border-bottom-width: %q", active.BorderBottomWidth)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestE2E_Tabs_OnlyOnePanelDisplayedAtATime(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var visibleCount int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		pageReady(),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.tabs > .tabs-panels > .tabs-panel')).filter(p => getComputedStyle(p).display !== 'none').length`, &visibleCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if visibleCount != 1 {
		t.Errorf("expected exactly 1 tab-panel visible, got %d", visibleCount)
	}
}

// Chaos — spam every tab summary 100 times. Only one panel should be
// visible at the end; only one details should be open.
func TestE2E_Chaos_TabsSpamClick(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var pair []int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/tabs"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const summaries = document.querySelectorAll('.tabs > details > summary');
            for (let i = 0; i < 300; i++) {
                summaries[i % summaries.length].click();
            }
            const open = document.querySelectorAll('.tabs > details[open]').length;
            const visiblePanels = Array.from(
                document.querySelectorAll('.tabs > .tabs-panels > .tabs-panel')
            ).filter(p => getComputedStyle(p).display !== 'none').length;
            return [open, visiblePanels];
        })()`, &pair),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(pair) != 2 {
		t.Fatalf("expected [openCount, visiblePanels], got %v", pair)
	}
	if pair[0] > 1 {
		t.Errorf("Tabs invariant broken: %d details open after spam-click", pair[0])
	}
	if pair[1] > 1 {
		t.Errorf("Tabs invariant broken: %d panels visible after spam-click", pair[1])
	}
}
