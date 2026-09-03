package runtime_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/testkit/axetest"
	"github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/chromedp/chromedp"
)

// Lazy-panel e2e for framework/ui.Menu's MenuConfig.LazyPanel: the SSR
// ships the panel's rows inside an inert <template
// data-fui-menu-lazy>, and the disclosure module inflates them before
// its focus-on-open lookup (and at scan time for an already-open
// details). Same package as menu_trigger_axe_e2e_test.go so the live
// framework/ui render and the real ui-menu stylesheet are available;
// the server helper below is that file's menuTriggerAxeServer, which
// serves the real runtime, the demand-loaded modules, and the
// production component CSS.

// lazyMenuItems is the shape every lazy fixtures render: an href row,
// a Palette submenu of theme radios, and a plain row whose label is
// unique enough to assert absence from page-scoped text queries.
func lazyMenuItems() []ui.MenuItem {
	return []ui.MenuItem{
		{Label: "Profile", Href: "/me"},
		{Label: "Palette", Children: []ui.MenuItem{
			{Label: "Light", Radio: "theme"},
			{Label: "Dark", Radio: "theme", Checked: true},
		}},
		{Label: "LazyOnlyRow"},
	}
}

func renderLazyMenu() string {
	return string(ui.Menu(ui.MenuConfig{
		ID:        "lm",
		Label:     "View",
		LazyPanel: true,
		Items:     lazyMenuItems(),
	}))
}

func renderLazyTriggerMenu() string {
	return string(ui.Menu(ui.MenuConfig{
		ID:             "lmt",
		TriggerElement: render.HTML(`<button type="button" class="rounded-full">Open lazy menu</button>`),
		LazyPanel:      true,
		Items:          lazyMenuItems(),
	}))
}

// lazyState snapshots everything the lazy contract asserts about one
// summary-path menu: open state, row counts (the panel's OWN rows via
// the same closest() scoping menu.js uses, and the descendant total
// that includes the submenu's radios), leftover templates, and the
// focused row's label.
type lazyState struct {
	Open      bool   `json:"open"`
	TotalRows int    `json:"totalRows"`
	OwnRows   int    `json:"ownRows"`
	Templates int    `json:"templates"`
	Focus     string `json:"focus"`
}

func lazyStateAction(dst *string) chromedp.Action {
	return chromedp.Evaluate(`JSON.stringify((() => {
		const panel = document.querySelector('#lm-panel');
		if (!panel) return {open: null, totalRows: -1, ownRows: -1, templates: -1, focus: ''};
		const rows = Array.from(panel.querySelectorAll('[role="menuitem"],[role="menuitemradio"]'));
		const own = rows.filter(n => n.closest('[role="menu"]') === panel);
		const el = document.activeElement;
		const l = el && el.querySelector ? el.querySelector(':scope > .ui-menu__label') : null;
		return {
			open: document.querySelector('details[data-fui-menu="lm"]').open,
			totalRows: rows.length,
			ownRows: own.length,
			templates: document.querySelectorAll('#lm-panel template').length,
			focus: l ? l.textContent.trim() : (el ? (el.textContent || '').trim() : '')
		};
	})())`, dst)
}

// lazyKey dispatches a keydown on the focused element, the same way
// menu_contract_e2e_test.go's menuKey does: the menu module's
// listeners are document-level and read e.target.
func lazyKey(key string) chromedp.Action {
	return chromedp.Evaluate(`document.activeElement.dispatchEvent(
		new KeyboardEvent('keydown', {key: `+quoteJS(key)+`, bubbles: true, cancelable: true}))`, nil)
}

func quoteJS(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

const lazyOpenSummary = `document.querySelector('details[data-fui-menu="lm"] > summary.ui-menu__trigger').click()`

// TestMenuLazyClosedPanelIsEmpty: while the menu is closed, the panel
// div exists with its id and role (aria-controls resolves), the lazy
// template is its ONLY child, and no row is visible to ANY page-scoped
// query — not role queries, not document.body.innerText, not a
// TreeWalker over the document's text nodes. This is the contract a
// host's getByText('Theme')/getByLabel('Theme') Playwright selectors
// rely on: a closed lazy menu contributes NO DOM.
func TestMenuLazyClosedPanelIsEmpty(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderLazyMenu())
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()

	var blob string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Menu + disclosure modules arrive via the idle marker scan.
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify((() => {
			const panel = document.querySelector('#lm-panel');
			let textNodeSeen = false;
			const w = document.createTreeWalker(document.body, NodeFilter.SHOW_TEXT);
			let n;
			while ((n = w.nextNode())) {
				if (n.textContent.includes('LazyOnlyRow')) { textNodeSeen = true; break; }
			}
			return {
				panelExists: !!panel,
				panelRole: panel ? panel.getAttribute('role') : null,
				panelChildren: panel ? panel.children.length : -1,
				onlyChild: panel && panel.children[0] ? panel.children[0].tagName : null,
				templates: document.querySelectorAll('#lm-panel template[data-fui-menu-lazy]').length,
				anyMenuItem: !!document.querySelector('#lm-panel [role="menuitem"], #lm-panel [role="menuitemradio"]'),
				rowTextInInnerText: document.body.innerText.includes('LazyOnlyRow'),
				rowTextInTextNodes: textNodeSeen
			};
		})())`, &blob),
	); err != nil {
		t.Fatal(err)
	}
	var got struct {
		PanelExists      bool   `json:"panelExists"`
		PanelRole        string `json:"panelRole"`
		PanelChildren    int    `json:"panelChildren"`
		OnlyChild        string `json:"onlyChild"`
		Templates        int    `json:"templates"`
		AnyMenuItem      bool   `json:"anyMenuItem"`
		RowTextInInner   bool   `json:"rowTextInInnerText"`
		RowTextInTextNod bool   `json:"rowTextInTextNodes"`
	}
	if err := json.Unmarshal([]byte(blob), &got); err != nil {
		t.Fatalf("state blob: %v (%s)", err, blob)
	}
	if !got.PanelExists || got.PanelRole != "menu" {
		t.Fatalf("panel div must exist with role=menu while closed (exists=%v role=%q) — aria-controls needs it", got.PanelExists, got.PanelRole)
	}
	if got.PanelChildren != 1 || got.OnlyChild != "TEMPLATE" || got.Templates != 1 {
		t.Fatalf("panel's only child must be the lazy template: children=%d first=%s templates=%d", got.PanelChildren, got.OnlyChild, got.Templates)
	}
	if got.AnyMenuItem {
		t.Fatal("a menuitem row is reachable by role query inside the closed panel — the lazy template leaked into the document tree")
	}
	if got.RowTextInInner || got.RowTextInTextNod {
		t.Fatalf("closed-menu row text is page-visible: innerText=%v textNode=%v — a host getByText contract would match the hidden row", got.RowTextInInner, got.RowTextInTextNod)
	}
}

// TestMenuLazySummaryOpenInflatesFocusesAndDoesNotDuplicate: the
// first open mounts the rows AND lands focus on the first one (the
// inflate must run BEFORE the disclosure module's focus-on-open
// lookup), and close + reopen mounts nothing twice: exactly the same
// row counts, no template left.
func TestMenuLazySummaryOpenInflatesFocusesAndDoesNotDuplicate(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderLazyMenu())
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()

	read := func() lazyState {
		t.Helper()
		var blob string
		if err := chromedp.Run(ctx, lazyStateAction(&blob)); err != nil {
			t.Fatal(err)
		}
		var s lazyState
		if err := json.Unmarshal([]byte(blob), &s); err != nil {
			t.Fatalf("state blob: %v (%s)", err, blob)
		}
		return s
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(lazyOpenSummary, nil),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}

	s := read()
	if !s.Open || s.OwnRows != 3 || s.TotalRows != 5 || s.Templates != 0 {
		t.Fatalf("after first open: %+v — want open, 3 own rows (Profile/Palette/LazyOnlyRow), 5 total (incl. submenu radios), 0 templates", s)
	}
	if s.Focus != "Profile" {
		t.Fatalf("focus after first open = %q, want Profile — the inflate must run before the focus-on-open lookup or the first open lands focus nowhere", s.Focus)
	}

	// Close with Escape, then reopen: the rows must not duplicate and
	// no template may remain (or reappear).
	if err := chromedp.Run(ctx,
		lazyKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(lazyOpenSummary, nil),
		chromedp.Sleep(150*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	s2 := read()
	if !s2.Open || s2.OwnRows != 3 || s2.TotalRows != 5 || s2.Templates != 0 {
		t.Fatalf("after close+reopen: %+v — want the SAME counts (3 own / 5 total / 0 templates): rows duplicated or template resurrected", s2)
	}
	if s2.Focus != "Profile" {
		t.Fatalf("focus after reopen = %q, want Profile", s2.Focus)
	}
}

// TestMenuLazyTriggerPathInflates: the TriggerElement path is lazy the
// same way. While closed, the menu module still wires aria-controls to
// the panel id (the panel div exists); the caller's button click opens
// the menu, mounts the rows, and lands focus on the first one.
func TestMenuLazyTriggerPathInflates(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderLazyTriggerMenu())
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()

	var controls string
	var blob string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelector('[data-fui-menu-trigger="lmt"] button').getAttribute('aria-controls'))`, &controls),
		// Closed: no row in the document tree on this path either.
		chromedp.Evaluate(`String(document.querySelector('#lmt-panel [role="menuitem"], #lmt-panel [role="menuitemradio"]') !== null)`, &blob),
	); err != nil {
		t.Fatal(err)
	}
	if controls != "lmt-panel" {
		t.Fatalf("aria-controls while closed = %q, want lmt-panel (wireTrigger reads the panel id, which must still exist)", controls)
	}
	if blob != "false" {
		t.Fatal("a row is reachable inside the closed trigger-path panel — the lazy template leaked")
	}

	if err := chromedp.Run(ctx,
		chromedp.Click(`button.rounded-full`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify((() => {
			const panel = document.querySelector('#lmt-panel');
			const rows = Array.from(panel.querySelectorAll('[role="menuitem"],[role="menuitemradio"]'));
			const own = rows.filter(n => n.closest('[role="menu"]') === panel);
			const el = document.activeElement;
			const l = el && el.querySelector ? el.querySelector(':scope > .ui-menu__label') : null;
			return {
				open: document.querySelector('details[data-fui-menu="lmt"]').open,
				ownRows: own.length,
				totalRows: rows.length,
				templates: document.querySelectorAll('#lmt-panel template').length,
				focus: l ? l.textContent.trim() : ''
			};
		})())`, &blob),
	); err != nil {
		t.Fatal(err)
	}
	var s struct {
		Open      bool   `json:"open"`
		OwnRows   int    `json:"ownRows"`
		TotalRows int    `json:"totalRows"`
		Templates int    `json:"templates"`
		Focus     string `json:"focus"`
	}
	if err := json.Unmarshal([]byte(blob), &s); err != nil {
		t.Fatalf("state blob: %v (%s)", err, blob)
	}
	if !s.Open || s.OwnRows != 3 || s.TotalRows != 5 || s.Templates != 0 {
		t.Fatalf("trigger path after click: %+v — want open, 3 own / 5 total rows, 0 templates", s)
	}
	if s.Focus != "Profile" {
		t.Fatalf("focus after trigger-path open = %q, want Profile", s.Focus)
	}
}

// TestMenuLazyKeyboardAfterMount: menu.js needs no lazy awareness —
// its keyboard handling, radio arbitration, and submenu opening are
// delegated or re-scanned, so they must all work on rows that mounted
// from the template. ArrowDown roves, type-ahead jumps by label, a
// radio click arbitrates the group, and ArrowRight opens a submenu
// that itself mounted lazily (it lived inside the template).
func TestMenuLazyKeyboardAfterMount(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderLazyMenu())
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()

	var afterDown, afterType, subLabel, radioMap, subOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(lazyOpenSummary, nil),
		chromedp.Sleep(150*time.Millisecond),
		lazyKey("ArrowDown"), // Profile -> Palette
		chromedp.Evaluate(`document.activeElement.querySelector(':scope > .ui-menu__label').textContent.trim()`, &afterDown),
		// Type-ahead from Palette: "l" jumps to LazyOnlyRow.
		lazyKey("l"),
		chromedp.Evaluate(`document.activeElement.querySelector(':scope > .ui-menu__label').textContent.trim()`, &afterType),
		// Back to Palette, then ArrowRight opens the (lazily mounted)
		// submenu and moves focus into it.
		lazyKey("ArrowUp"),
		lazyKey("ArrowRight"),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="lm-panel-sub-1"]').open)`, &subOpen),
		chromedp.Evaluate(`(() => {
			const el = document.activeElement;
			const l = el.querySelector(':scope > .ui-menu__label');
			return (l ? l.textContent : el.textContent).trim();
		})()`, &subLabel),
		// Roving inside the mounted submenu: Light -> Dark.
		lazyKey("ArrowDown"),
		// Radio arbitration on mounted rows: activate Light; Dark (the
		// SSR-checked sibling) must lose the check.
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#lm-panel-sub-1-panel [role="menuitemradio"]')).find(r => r.textContent.includes('Light')).click()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`JSON.stringify(Array.from(document.querySelectorAll('#lm-panel-sub-1-panel [role="menuitemradio"]')).map(r => r.querySelector('.ui-menu__label').textContent.trim() + ':' + r.getAttribute('aria-checked')))`, &radioMap),
	); err != nil {
		t.Fatal(err)
	}
	if afterDown != "Palette" {
		t.Fatalf("ArrowDown on mounted panel = %q, want Palette", afterDown)
	}
	if afterType != "LazyOnlyRow" {
		t.Fatalf("type-ahead 'l' = %q, want LazyOnlyRow", afterType)
	}
	if subOpen != "true" || subLabel != "Light" {
		t.Fatalf("ArrowRight on lazily-mounted submenu: open=%s focus=%q, want true/Light", subOpen, subLabel)
	}
	if radioMap != `["Light:true","Dark:false"]` {
		t.Fatalf("radio arbitration on mounted rows = %s, want Light checked and Dark unchecked", radioMap)
	}
}

// TestMenuLazyPreloadedOpenScanInflates: the race — the details is
// already open in the SSR bytes BEFORE the runtime loads (a click that
// beat the idle load, or a hand-authored open menu). No toggle event
// fires after the disclosure module arrives, so its initial scan()
// pass must inflate the lazy rows.
func TestMenuLazyPreloadedOpenScanInflates(t *testing.T) {
	body := strings.Replace(renderLazyMenu(), `data-fui-menu="lm"`, `data-fui-menu="lm" open`, 1)
	srv := menuTriggerAxeServer(t, body)
	browser := axetest.NewBrowser(t)
	ctx, cancel := axetest.NewTab(t, browser)
	defer cancel()

	var blob string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(900*time.Millisecond),
		lazyStateAction(&blob),
	); err != nil {
		t.Fatal(err)
	}
	var s lazyState
	if err := json.Unmarshal([]byte(blob), &s); err != nil {
		t.Fatalf("state blob: %v (%s)", err, blob)
	}
	if !s.Open || s.OwnRows != 3 || s.TotalRows != 5 || s.Templates != 0 {
		t.Fatalf("pre-opened lazy menu after module load: %+v — want open with mounted rows (3 own / 5 total / 0 templates); the scan pass did not inflate", s)
	}
}

// menuLazyAxeAllowlist carries the one rule the lazy scan tolerates,
// with the reason inline (same discipline as
// menuTriggerAxeAllowlist). aria-allowed-role fires on
// <summary role="menuitem"> — the pre-existing submenu-parent dialect
// framework/ui renders for MenuItem.Children, identical on the
// non-lazy summary path and out of scope for the lazy-panel contract.
var menuLazyAxeAllowlist = map[string]string{
	"aria-allowed-role": "summary role=menuitem is the existing submenu-parent dialect, not the lazy-panel surface",
}

// TestMenuLazyAxeClean: the lazy menu must scan clean closed and open,
// under both color schemes, with the real component CSS applied. While
// closed the panel is empty and display:none (axe excludes hidden
// content), so the inert template shape must not invent violations;
// once opened the rows are ordinary menu markup.
func TestMenuLazyAxeClean(t *testing.T) {
	srv := menuTriggerAxeServer(t, renderLazyMenu())
	browser := axetest.NewBrowser(t)
	for _, scheme := range axetest.Schemes {
		ctx, cancel := axetest.NewTab(t, browser)
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+"/"),
			chromedp.WaitVisible(`#ready`, chromedp.ByID),
			chromedp.Sleep(700*time.Millisecond),
			axetest.Prepare(scheme),
		); err != nil {
			cancel()
			t.Fatalf("axe setup (%s): %v", scheme, err)
		}
		vs, err := axetest.Scan(ctx, scheme, nil)
		cancel()
		if err != nil {
			t.Fatalf("axe scan closed (%s): %v", scheme, err)
		}
		for _, v := range vs {
			if reason, ok := menuLazyAxeAllowlist[v.ID]; ok {
				t.Logf("(%s, closed) allowlisted %s: %s", scheme, v.ID, reason)
				continue
			}
			t.Errorf("closed lazy menu (%s scheme): [%s] %s", scheme, v.ID, v.Help)
		}

		// Same page with the menu OPEN: rows mounted and visible.
		ctx2, cancel2 := axetest.NewTab(t, browser)
		if err := chromedp.Run(ctx2,
			chromedp.Navigate(srv.URL+"/"),
			chromedp.WaitVisible(`#ready`, chromedp.ByID),
			chromedp.Sleep(700*time.Millisecond),
			axetest.Prepare(scheme),
			chromedp.Click(`summary.ui-menu__trigger`, chromedp.ByQuery),
			chromedp.Sleep(250*time.Millisecond),
		); err != nil {
			cancel2()
			t.Fatalf("axe open setup (%s): %v", scheme, err)
		}
		vs2, err := axetest.Scan(ctx2, scheme, nil)
		cancel2()
		if err != nil {
			t.Fatalf("axe scan open (%s): %v", scheme, err)
		}
		for _, v := range vs2 {
			if reason, ok := menuLazyAxeAllowlist[v.ID]; ok {
				t.Logf("(%s, open) allowlisted %s: %s", scheme, v.ID, reason)
				continue
			}
			t.Errorf("open lazy menu (%s scheme): [%s] %s", scheme, v.ID, v.Help)
		}
	}
}
