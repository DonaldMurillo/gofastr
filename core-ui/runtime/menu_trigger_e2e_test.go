package runtime

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// menuTriggerFixture is the exact SSR of a framework/ui.Menu rendered
// with MenuConfig.TriggerElement: the caller's own
// button.rounded-full inside a [data-fui-menu-trigger] presentation
// wrapper, BESIDE a summary-less <details data-fui-menu> holding the
// panel (an href row, then a Palette submenu of theme radios). Same
// discipline as menuContractFixture: bytes captured from the component
// renderer and pinned byte-for-byte by framework/ui's
// TestMenuTriggerGoldenBytes. It cannot be rendered live here: package
// runtime's test binary importing framework/ui is an import cycle
// (framework/ui links core-ui/runtime), so the axe suite in
// menu_trigger_axe_e2e_test.go — an external runtime_test package —
// owns the live-rendered fixtures.
const menuTriggerFixture = `<div class="ui-menu ui-menu--bottom-start" data-fui-comp="ui-menu"><div data-fui-menu-trigger="um" role="presentation"><button type="button" class="rounded-full">Open user menu</button></div><details data-fui-disclosure data-fui-menu="um"><div class="ui-menu__panel" id="um-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/me" role="menuitem" tabindex="-1"><span class="ui-menu__label">Profile</span></a><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-1"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="um-panel-sub-1-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Palette</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-1-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Light</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="theme"><span class="ui-menu__label">Dark</span></button></div></details></div></details></div>`

// menuTriggerOpen reports the trigger menu's open state.
func menuTriggerOpen(dst *string) chromedp.Action {
	return chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um"]').open)`, dst)
}

// menuTriggerSubOpen reports the Palette submenu's open state.
func menuTriggerSubOpen(dst *string) chromedp.Action {
	return chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um-panel-sub-1"]').open)`, dst)
}

// menuTriggerAttr reads an attribute off the caller's button.
func menuTriggerAttr(dst *string, name string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(
		`String(document.querySelector('[data-fui-menu-trigger="um"] button').getAttribute(%q))`, name), dst)
}

// menuTriggerClick clicks the caller's button (a JS click, like
// menuOpenTop for the summary path — the menu module's delegated
// listener fires identically).
const menuTriggerClick = `document.querySelector('[data-fui-menu-trigger="um"] button').click()`

// menuTriggerFocused reports whether focus sits on the caller's button.
func menuTriggerFocused(dst *string) chromedp.Action {
	return chromedp.Evaluate(`String(document.activeElement === document.querySelector('[data-fui-menu-trigger="um"] button'))`, dst)
}

// TestMenuTriggerAriaWiredOnLoad: the runtime wires
// aria-haspopup="menu", aria-controls (the panel's id), and
// aria-expanded="false" onto the caller's element at hydration —
// attributes that cannot be injected into raw caller HTML server-side.
func TestMenuTriggerAriaWiredOnLoad(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerFixture)
	ctx := newSeedBrowserCtx(t)

	var haspopup, controls, expanded string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Menu + disclosure modules arrive via the idle marker scan.
		chromedp.Sleep(700*time.Millisecond),
		menuTriggerAttr(&haspopup, "aria-haspopup"),
		menuTriggerAttr(&controls, "aria-controls"),
		menuTriggerAttr(&expanded, "aria-expanded"),
	); err != nil {
		t.Fatal(err)
	}
	if haspopup != "menu" {
		t.Errorf("aria-haspopup = %q, want menu", haspopup)
	}
	if controls != "um-panel" {
		t.Errorf("aria-controls = %q, want um-panel", controls)
	}
	if expanded != "false" {
		t.Errorf("aria-expanded = %q, want false on load", expanded)
	}
}

// TestMenuTriggerClickOpensAndFocuses is the pixels-not-probes proof of
// the trigger-element contract: a real pointer click on the caller's
// button opens the menu, focus lands on the FIRST menuitem, aria
// follows on the caller's element, and Escape closes and returns focus
// to the caller's button.
func TestMenuTriggerClickOpensAndFocuses(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerFixture)
	ctx := newSeedBrowserCtx(t)

	var open, expanded, label string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		chromedp.Click(`button.rounded-full`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&open),
		menuTriggerAttr(&expanded, "aria-expanded"),
		menuActiveLabel(&label),
	); err != nil {
		t.Fatal(err)
	}
	if open != "true" {
		t.Fatalf("menu open after real click = %s, want true", open)
	}
	if expanded != "true" {
		t.Fatalf("aria-expanded after open = %s, want true", expanded)
	}
	if label != "Profile" {
		t.Fatalf("focus after open = %q, want Profile (first menuitem)", label)
	}

	// Escape closes the menu and returns focus to the CALLER's button —
	// there is no summary for the disclosure module's default return.
	var openAfter, expandedAfter, focusOnBtn string
	if err := chromedp.Run(ctx,
		menuKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&openAfter),
		menuTriggerAttr(&expandedAfter, "aria-expanded"),
		menuTriggerFocused(&focusOnBtn),
	); err != nil {
		t.Fatal(err)
	}
	if openAfter != "false" {
		t.Fatalf("menu open after Escape = %s, want false", openAfter)
	}
	if expandedAfter != "false" {
		t.Fatalf("aria-expanded after Escape = %s, want false", expandedAfter)
	}
	if focusOnBtn != "true" {
		t.Fatal("focus after Escape must return to the caller's trigger button")
	}
}

// TestMenuTriggerEnterAndSpaceOpen: keyboard activation parity with the
// summary path — real Enter and Space keypresses on the caller's
// button (native button activation produces the click) open the menu.
func TestMenuTriggerEnterAndSpaceOpen(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerFixture)
	ctx := newSeedBrowserCtx(t)

	var open, expanded string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		chromedp.Focus(`button.rounded-full`, chromedp.ByQuery),
		chromedp.SendKeys(`button.rounded-full`, kb.Enter, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&open),
	); err != nil {
		t.Fatal(err)
	}
	if open != "true" {
		t.Fatalf("menu open after Enter = %s, want true", open)
	}

	// Close, then Space reopens.
	if err := chromedp.Run(ctx,
		menuKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&open),
	); err != nil {
		t.Fatal(err)
	}
	if open != "false" {
		t.Fatalf("menu open after Escape = %s, want false", open)
	}
	if err := chromedp.Run(ctx,
		chromedp.SendKeys(`button.rounded-full`, " ", chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&open),
		menuTriggerAttr(&expanded, "aria-expanded"),
	); err != nil {
		t.Fatal(err)
	}
	if open != "true" {
		t.Fatalf("menu open after Space = %s, want true", open)
	}
	if expanded != "true" {
		t.Fatalf("aria-expanded after Space = %s, want true", expanded)
	}
}

// TestMenuTriggerEscapeClosesOneLevel: Escape parity at depth — from
// inside the submenu only the submenu closes (focus returns to the
// Palette row, top stays open), and the next Escape closes the top and
// returns focus to the caller's button.
func TestMenuTriggerEscapeClosesOneLevel(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerFixture)
	ctx := newSeedBrowserCtx(t)

	var label, subOpen, topOpen, focusOnBtn string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		chromedp.Evaluate(menuTriggerClick, nil), // open; focus on Profile
		chromedp.Sleep(150*time.Millisecond),
		menuKey("ArrowDown"), // Profile -> Palette
		menuActiveLabel(&label),
		menuKey("ArrowRight"), // open submenu; focus Light
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerSubOpen(&subOpen),
	); err != nil {
		t.Fatal(err)
	}
	if label != "Palette" {
		t.Fatalf("ArrowDown landed on %q, want Palette", label)
	}
	if subOpen != "true" {
		t.Fatalf("submenu open after ArrowRight = %s, want true", subOpen)
	}

	// First Escape: only the submenu closes, focus back on Palette.
	if err := chromedp.Run(ctx,
		menuKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerSubOpen(&subOpen),
		menuTriggerOpen(&topOpen),
		menuActiveLabel(&label),
	); err != nil {
		t.Fatal(err)
	}
	if subOpen != "false" || topOpen != "true" {
		t.Fatalf("first Escape: sub=%s top=%s, want false/true (one level at a time)", subOpen, topOpen)
	}
	if label != "Palette" {
		t.Fatalf("focus after first Escape = %q, want Palette", label)
	}

	// Second Escape: the top closes and focus lands on the CALLER's
	// button (the summary-less disclosure's controller).
	if err := chromedp.Run(ctx,
		menuKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&topOpen),
		menuTriggerFocused(&focusOnBtn),
	); err != nil {
		t.Fatal(err)
	}
	if topOpen != "false" {
		t.Fatalf("second Escape: top=%s, want false", topOpen)
	}
	if focusOnBtn != "true" {
		t.Fatal("focus after second Escape must return to the caller's trigger button")
	}
}

// TestMenuTriggerTabCloses: Tab-through close, both shapes — from a
// focused menuitem the whole chain closes (the menuitem Tab branch),
// and from the caller's own trigger button the menu closes before
// focus moves on (menuitems are tabindex=-1; Tab would otherwise jump
// past the panel and strand the menu open).
func TestMenuTriggerTabCloses(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerFixture)
	ctx := newSeedBrowserCtx(t)

	var topOpen, subOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		chromedp.Evaluate(menuTriggerClick, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuKey("ArrowDown"), // Profile -> Palette
		menuKey("ArrowRight"),
		chromedp.Sleep(150*time.Millisecond),
		menuKey("Tab"),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerSubOpen(&subOpen),
		menuTriggerOpen(&topOpen),
	); err != nil {
		t.Fatal(err)
	}
	if subOpen != "false" || topOpen != "false" {
		t.Fatalf("Tab from inside: sub=%s top=%s, want false/false (whole chain closes)", subOpen, topOpen)
	}

	// Reopen, put focus back on the caller's button, Tab closes too.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(menuTriggerClick, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Focus(`button.rounded-full`, chromedp.ByQuery),
		menuKey("Tab"),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&topOpen),
	); err != nil {
		t.Fatal(err)
	}
	if topOpen != "false" {
		t.Fatal("Tab from the caller's trigger must close its menu")
	}
}

// menuTriggerAnchorFixture is the same menu with an <a> as the
// caller's trigger: Space does not activate a native anchor, so the
// module has to open the menu itself (and keep the page from
// scrolling).
var menuTriggerAnchorFixture = strings.Replace(menuTriggerFixture,
	`<button type="button" class="rounded-full">Open user menu</button>`,
	`<a href="/account" class="rounded-full">Open user menu</a>`, 1)

func TestMenuTriggerAnchorOpensOnSpace(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerAnchorFixture)
	ctx := newSeedBrowserCtx(t)
	var open, href string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Focus(`a.rounded-full`, chromedp.ByQuery),
		chromedp.SendKeys(`a.rounded-full`, " ", chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&open),
		chromedp.Evaluate(`location.pathname`, &href),
	); err != nil {
		t.Fatal(err)
	}
	if open != "true" {
		t.Fatalf("anchor trigger: menu open after Space = %s, want true", open)
	}
	if href != "/" {
		t.Fatalf("Space on the anchor trigger navigated to %s; activation must be prevented", href)
	}
}

// Tab from the trigger closes the whole chain: a submenu left open
// inside a closed root would reappear expanded on the next open.
func TestMenuTriggerTabClosesSubmenuToo(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuTriggerFixture)
	ctx := newSeedBrowserCtx(t)
	var topOpen, subOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(menuTriggerClick, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="um-panel-sub-1"]').setAttribute('open','')`, nil),
		chromedp.Focus(`button.rounded-full`, chromedp.ByQuery),
		chromedp.SendKeys(`button.rounded-full`, kb.Tab, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		menuTriggerOpen(&topOpen),
		menuTriggerSubOpen(&subOpen),
	); err != nil {
		t.Fatal(err)
	}
	if topOpen != "false" || subOpen != "false" {
		t.Fatalf("after Tab from the trigger: top open = %s, submenu open = %s, want both false", topOpen, subOpen)
	}
}
