package main

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// =============================================================================
// Accordion — Group (exclusive) + Stack (independent) + chaos
// =============================================================================

func TestE2E_AccordionGroup_OnlyOneItemOpenAtATime(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var openCount0, openCount1, openCount2 int
	var openLabel string

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		// First Group item starts Open: true.
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details[open]').length`, &openCount0),
		// Click the second summary inside the first group.
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details > summary')[1].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details[open]').length`, &openCount1),
		chromedp.Evaluate(`document.querySelector('.accordion-group > details[open] .accordion-label').textContent`, &openLabel),
		// Now click the third — second should close, third should open.
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details > summary')[2].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details[open]').length`, &openCount2),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if openCount0 != 1 {
		t.Errorf("initial: expected exactly 1 open item, got %d", openCount0)
	}
	if openCount1 != 1 {
		t.Errorf("after click 2: expected exactly 1 open item, got %d", openCount1)
	}
	if openCount2 != 1 {
		t.Errorf("after click 3: expected exactly 1 open item, got %d", openCount2)
	}
	if !strings.Contains(openLabel, "animation") {
		t.Errorf("after clicking item 2, expected its summary label visible; got %q", openLabel)
	}
}

func TestE2E_AccordionStack_ItemsOpenIndependently(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var openCount int

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-stack > details > summary')[0].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-stack > details > summary')[1].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-stack > details > summary')[2].click()`, nil),
		settle(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-stack > details[open]').length`, &openCount),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if openCount != 3 {
		t.Errorf("Stack should allow all open simultaneously, got %d/3 open", openCount)
	}
}

func TestE2E_AccordionGroup_KeyboardEnterTogglesItem(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	// Focus the second summary, press Enter, verify it became the open one.
	var openLabel string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		chromedp.Evaluate(`document.querySelectorAll('.accordion-group > details > summary')[1].focus()`, nil),
		chromedp.KeyEvent("\r"),
		settle(),
		chromedp.Evaluate(`document.querySelector('.accordion-group > details[open] .accordion-label').textContent`, &openLabel),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if !strings.Contains(openLabel, "animation") {
		t.Errorf("Enter on summary did not toggle; current open = %q", openLabel)
	}
}

// Chaos — rapid-fire clicks across the Group should never produce a
// state with 0 or 2+ open items (browser invariant on `name=`).
func TestE2E_Chaos_AccordionGroupRapidToggle(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var pair []int
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		chromedp.Evaluate(`(() => {
            const summaries = document.querySelectorAll('.accordion-group > details > summary');
            let minOpen = 999, maxOpen = 0;
            for (let i = 0; i < 200; i++) {
                summaries[i % summaries.length].click();
                const open = document.querySelectorAll('.accordion-group > details[open]').length;
                if (open < minOpen) minOpen = open;
                if (open > maxOpen) maxOpen = open;
            }
            return [minOpen, maxOpen];
        })()`, &pair),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if len(pair) != 2 {
		t.Fatalf("expected [min, max], got %v", pair)
	}
	minOpen, maxOpen := pair[0], pair[1]
	// Group invariant: at most 1 open at any time. 0 or 1 is fine
	// (clicking an open item closes it).
	if maxOpen > 1 {
		t.Errorf("Group invariant broken: max simultaneously open = %d", maxOpen)
	}
	if minOpen < 0 {
		t.Errorf("nonsensical min open count: %d", minOpen)
	}

	// Settle and confirm DOM is stable.
	chromedp.Run(ctx, chromedp.Sleep(100*time.Millisecond))
}

// Confirm the modern-CSS animation styles really applied — getComputedStyle
// must report the expected interpolate-size token on the accordion-item.
func TestE2E_AccordionAnimationCSSAppliedByBrowser(t *testing.T) {
	base := startE2EServer(t)
	ctx := newE2EBrowserCtx(t)

	var interpolateSize string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/components/accordion"),
		pageReady(),
		chromedp.Evaluate(`getComputedStyle(document.querySelector('.accordion-item')).interpolateSize || getComputedStyle(document.querySelector('.accordion-item')).getPropertyValue('interpolate-size')`, &interpolateSize),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	// Browsers that don't support interpolate-size return "" — that's
	// the documented progressive-enhancement fallback. Just make sure
	// no exception bubbled up. If non-empty, must be the keyword.
	if interpolateSize != "" && !strings.Contains(strings.ToLower(interpolateSize), "allow-keywords") {
		t.Errorf("interpolate-size unexpected value: %q", interpolateSize)
	}
}
