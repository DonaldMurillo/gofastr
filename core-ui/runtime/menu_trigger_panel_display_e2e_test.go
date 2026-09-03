package runtime_test

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

// This suite pins the COMPUTED display of real menu panels under the
// real component stylesheet (served from the registry by
// menuTriggerAxeServer), not the stylesheet's text. Issue #386: the
// root-keyed closed-panel rule used to key [open] on whatever element
// carried data-fui-comp, but on the TriggerElement path that root is a
// plain div, which never carries [open] — the rule matched
// unconditionally and kept the panel display:none even while the
// summary-less details was open. Every CSS-text test passed throughout
// (the selector text was present and wrong); only the computed style of
// a rendered menu can see the difference. Fixtures render live from
// framework/ui for the same layering reason as the axe suite: the
// internal package cannot import framework/ui (import cycle).

// menuPanelDisplay reads the computed display of the panel with the
// given id.
func menuPanelDisplay(dst *string, id string) chromedp.Action {
	return chromedp.Evaluate(`getComputedStyle(document.querySelector('#`+id+`')).display`, dst)
}

// menuPanelDetailsOpen reports the open state of the details carrying
// the given data-fui-menu value.
func menuPanelDetailsOpen(dst *string, menu string) chromedp.Action {
	return chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="`+menu+`"]').open)`, dst)
}

// renderSummaryMenu renders a live summary-path Menu (no
// TriggerElement): the root IS the <details>, so the root-keyed
// closed-panel rule keys [open] on it directly. Pinned beside the
// trigger path so the fix that scopes that rule to a details root
// cannot regress the path it was written for.
func renderSummaryMenu() string {
	return string(ui.Menu(ui.MenuConfig{
		ID:    "sm",
		Label: "Account",
		Items: []ui.MenuItem{
			{Label: "Profile", Href: "/me"},
			{Label: "Palette", Children: []ui.MenuItem{
				{Label: "Light", Radio: "theme"},
				{Label: "Dark", Radio: "theme", Checked: true},
			}},
		},
	}))
}

// TestMenuTriggerPanelDisplay pins the top-level panel contract on both
// menu shapes, under the real stylesheet: computed display is "none"
// while the disclosure is closed and not "none" while it is open. Each
// checkpoint gets its own Run + immediate assert so later actions
// cannot overwrite shared state before the assert runs.
func TestMenuTriggerPanelDisplay(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderTriggerMenu()+"\n"+renderSummaryMenu())
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Hydration + the runtime's scanAndLoadCSS fetch of
		// /__gofastr/comp/ui-menu.css must both finish before any
		// computed display is meaningful.
		chromedp.Sleep(700*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}

	// Trigger path, closed: the summary-less details child carries the
	// hiding (details[data-fui-menu]:not([open]) rule).
	var disp string
	if err := chromedp.Run(ctx, menuPanelDisplay(&disp, "um-panel")); err != nil {
		t.Fatal(err)
	}
	if disp != "none" {
		t.Fatalf("trigger menu closed: #um-panel computed display = %q, want %q", disp, "none")
	}

	// Summary path, closed: the details root carries the hiding.
	if err := chromedp.Run(ctx, menuPanelDisplay(&disp, "sm-panel")); err != nil {
		t.Fatal(err)
	}
	if disp != "none" {
		t.Fatalf("summary menu closed: #sm-panel computed display = %q, want %q", disp, "none")
	}

	// Trigger path, open: the caller's button click (the menu module's
	// delegated listener, same as menuTriggerClick) opens the
	// summary-less details. The root div never carries [open], so no
	// closed-panel rule may still match the panel: computed display
	// must leave "none" (the .ui-menu__panel rule sets grid).
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-fui-menu-trigger="um"] button').click()`, nil),
		chromedp.Sleep(250*time.Millisecond),
		menuPanelDetailsOpen(&disp, "um"),
	); err != nil {
		t.Fatal(err)
	}
	if disp != "true" {
		t.Fatalf("trigger menu open state = %q, want %q (click did not open the details)", disp, "true")
	}
	if err := chromedp.Run(ctx, menuPanelDisplay(&disp, "um-panel")); err != nil {
		t.Fatal(err)
	}
	if disp == "none" {
		t.Fatal("trigger menu OPEN: #um-panel computed display = \"none\" — a closed-panel rule matches the div root's descendant panel while the details is open (issue #386)")
	}

	// Summary path, open: same contract through the summary toggle.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="sm"] > summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(250*time.Millisecond),
		menuPanelDetailsOpen(&disp, "sm"),
	); err != nil {
		t.Fatal(err)
	}
	if disp != "true" {
		t.Fatalf("summary menu open state = %q, want %q (click did not open the details)", disp, "true")
	}
	if err := chromedp.Run(ctx, menuPanelDisplay(&disp, "sm-panel")); err != nil {
		t.Fatal(err)
	}
	if disp == "none" {
		t.Fatal("summary menu OPEN: #sm-panel computed display = \"none\" — the details-scoped closed-panel rule still matches an open details root")
	}
}

// menuPanelVisible reports the browser's own visibility verdict for
// the panel with the given id (checkVisibility: false for
// display:none, a content-visibility-hidden ancestor, etc).
func menuPanelVisible(dst *string, id string) chromedp.Action {
	return chromedp.Evaluate(`String(document.querySelector('#`+id+`').checkVisibility())`, dst)
}

// TestMenuTriggerPanelSubmenuDisplay pins the nested contract: inside an
// OPEN trigger menu, a closed submenu (MenuItem.Children) panel is not
// visible and an opened submenu is. The submenu details is a descendant
// (not a child) of the div root and carries no author hide rule of its
// own: on current Chrome the UA sheet's ::details-content
// content-visibility hides a closed submenu's content WITHOUT touching
// the panel's own computed display (probed: display stays "grid" while
// checkVisibility() is false), so the closed state is pinned through
// checkVisibility rather than computed display. Before the #386 fix
// none of that mattered: the root-keyed rule display:none'd every
// nested panel unconditionally, so an OPENED submenu stayed hidden —
// the opened-state assertions below are the regression guard, and they
// also demand computed display leave "none".
func TestMenuTriggerPanelSubmenuDisplay(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderTriggerMenu())
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('[data-fui-menu-trigger="um"] button').click()`, nil),
		chromedp.Sleep(250*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	var open string
	if err := chromedp.Run(ctx, menuPanelDetailsOpen(&open, "um")); err != nil {
		t.Fatal(err)
	}
	if open != "true" {
		t.Fatalf("trigger menu open state = %q, want %q", open, "true")
	}

	// Submenu closed while its parent menu is open: not visible (the
	// browser's own verdict — display:none OR the UA sheet's
	// content-visibility hiding), and its rows are not visible either.
	var vis, rowVis string
	if err := chromedp.Run(ctx,
		menuPanelVisible(&vis, "um-panel-sub-1-panel"),
		chromedp.Evaluate(`String(document.querySelector('#um-panel-sub-1-panel [role="menuitemradio"]').checkVisibility())`, &rowVis),
	); err != nil {
		t.Fatal(err)
	}
	if vis != "false" {
		t.Fatalf("submenu closed inside open trigger menu: #um-panel-sub-1-panel visible = %s, want false (a closed submenu must not render)", vis)
	}
	if rowVis != "false" {
		t.Fatalf("submenu closed inside open trigger menu: radio row visible = %s, want false", rowVis)
	}

	// Open the submenu through its own summary row.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="um-panel-sub-1"] > summary').click()`, nil),
		chromedp.Sleep(250*time.Millisecond),
		menuPanelDetailsOpen(&open, "um-panel-sub-1"),
	); err != nil {
		t.Fatal(err)
	}
	if open != "true" {
		t.Fatalf("submenu open state = %q, want %q (click did not open the submenu)", open, "true")
	}
	var disp string
	if err := chromedp.Run(ctx,
		menuPanelDisplay(&disp, "um-panel-sub-1-panel"),
		menuPanelVisible(&vis, "um-panel-sub-1-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if disp == "none" {
		t.Fatal("submenu OPEN inside open trigger menu: #um-panel-sub-1-panel computed display = \"none\" — a closed-panel rule matches nested panels regardless of the submenu's own disclosure state")
	}
	if vis != "true" {
		t.Fatalf("submenu OPEN inside open trigger menu: #um-panel-sub-1-panel visible = %s, want true", vis)
	}
}
