package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// menuContractFixture is the exact SSR of a framework/ui.Menu carrying
// every shape this contract covers: plain rows, a Palette submenu of
// menuitemradio rows (theme group, Dark checked, Dark carrying an icon
// so type-ahead has to skip real icon TEXT and not just pseudo-element
// content), a separator, a second radio group in its own submenu
// (density), and a Disabled submenu parent whose children must stay
// unreachable. Captured from the component renderer so the fixture
// cannot drift from production markup (same discipline as the tabs
// contract fixture).
const menuContractFixture = `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="um" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="um-panel">Account<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="um-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/me" role="menuitem" tabindex="-1"><span class="ui-menu__label">Profile</span></a><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-1"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="um-panel-sub-1-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Palette</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-1-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Light</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Auto</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="theme"><span class="ui-menu__icon" aria-hidden="true">◐</span><span class="ui-menu__label">Dark</span></button></div></details><hr class="ui-menu__sep" role="separator"><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-3"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="um-panel-sub-3-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Density</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-3-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="density"><span class="ui-menu__label">Cozy</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="density"><span class="ui-menu__label">Compact</span></button></div></details><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-4"><summary class="ui-menu__item ui-menu__item--hassub ui-menu__item--disabled" aria-haspopup="menu" aria-controls="um-panel-sub-4-panel" role="menuitem" tabindex="-1" aria-disabled="true"><span class="ui-menu__label">Locked</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-4-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Hidden1</span></button><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Hidden2</span></button></div></details></div></details>`

// menuKey dispatches a keydown on the focused element, the same way the
// site's menu e2e does: the menu module's listeners are document-level
// and read e.target, so a bubbling synthetic event exercises them.
func menuKey(key string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(
		`document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:%q,bubbles:true,cancelable:true}))`, key), nil)
}

// menuActiveLabel reads the focused row's label span into dst (empty
// when focus is elsewhere).
func menuActiveLabel(dst *string) chromedp.Action {
	return chromedp.Evaluate(`(() => {
		const el = document.activeElement;
		if (!el) return '';
		const l = el.querySelector(':scope > .ui-menu__label');
		return (l ? l.textContent : el.textContent).trim();
	})()`, dst)
}

// menuSubOpen reports the Palette submenu's open state.
func menuSubOpen(dst *string) chromedp.Action {
	return chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um-panel-sub-1"]').open)`, dst)
}

// menuTopOpen reports the top-level disclosure's open state (the root
// carries data-fui-menu="um", not an id).
func menuTopOpen(dst *string) chromedp.Action {
	return chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um"]').open)`, dst)
}

// menuCheckedMap snapshots every radio row of a panel into dst as
// "Label:checked" pairs.
func menuCheckedMap(dst *string, panelSel string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`JSON.stringify(Array.from(
		document.querySelectorAll('%s [role="menuitemradio"]')
	).map(r => r.querySelector('.ui-menu__label').textContent.trim() + ':' + r.getAttribute('aria-checked')))`, panelSel), dst)
}

// menuClickRadio clicks the row whose label contains the given text.
func menuClickRadio(panelSel, text string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(
		`Array.from(document.querySelectorAll('%s [role="menuitemradio"]')).find(r => r.textContent.includes(%q)).click()`, panelSel, text), nil)
}

const menuOpenTop = `document.querySelector('details[data-fui-menu="um"] > summary.ui-menu__trigger').click()`

// TestMenuSubmenuKeyboardContract pins the keyboard contract markup
// alone cannot give: ArrowRight opens the submenu and moves focus into
// it, ArrowLeft closes it and returns focus to the parent row, roving
// focus WRAPS WITHIN the submenu (rows scoped by
// closest('[role=menu]') === panel, not the flat document order a
// submenu would otherwise leak into), Escape closes one level at a
// time, and Tab closes the whole chain. Checkpoints get their own Run
// + immediate assert: reading state into shared vars across a longer
// Run means later actions overwrite them before the asserts ever run.
func TestMenuSubmenuKeyboardContract(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var afterOpen, afterDown, afterDown2, subOpen, expanded, label string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Menu + disclosure modules arrive via the idle marker scan.
		chromedp.Sleep(700*time.Millisecond),

		// Open the top menu: focus lands on the first row.
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuActiveLabel(&afterOpen),
		menuSubOpen(&subOpen),
		menuKey("ArrowDown"),
		menuActiveLabel(&afterDown),
		// Top-level rotation must skip the whole submenu subtree:
		// Palette's successor is Density, never Light.
		menuKey("ArrowDown"),
		menuActiveLabel(&afterDown2),
	); err != nil {
		t.Fatal(err)
	}
	if afterOpen != "Profile" {
		t.Fatalf("focus after open = %q, want Profile", afterOpen)
	}
	if afterDown != "Palette" {
		t.Fatalf("ArrowDown from Profile = %q, want Palette", afterDown)
	}
	if afterDown2 != "Density" {
		t.Fatalf("ArrowDown from Palette = %q, want Density (submenu rows must not leak into the parent rotation)", afterDown2)
	}
	if subOpen != "false" {
		t.Fatalf("Palette submenu open before ArrowRight = %s, want false", subOpen)
	}

	// Return focus to Palette for the ArrowRight checkpoint below.
	if err := chromedp.Run(ctx, menuKey("ArrowUp")); err != nil {
		t.Fatal(err)
	}

	// ArrowRight opens the submenu and moves focus into it.
	if err := chromedp.Run(ctx,
		menuKey("ArrowRight"),
		chromedp.Sleep(120*time.Millisecond),
		menuSubOpen(&subOpen),
		menuActiveLabel(&label),
		chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um-panel-sub-1"] > summary').getAttribute('aria-expanded'))`, &expanded),
	); err != nil {
		t.Fatal(err)
	}
	if subOpen != "true" || expanded != "true" {
		t.Fatalf("after ArrowRight: sub open=%s aria-expanded=%s, want true/true", subOpen, expanded)
	}
	if label != "Light" {
		t.Fatalf("focus after ArrowRight = %q, want Light", label)
	}

	// Roving focus wraps WITHIN the submenu. Without the closest()
	// scoping, ArrowDown from Dark would walk into the Density rows.
	var d1, d2, d3 string
	if err := chromedp.Run(ctx,
		menuKey("ArrowDown"), menuActiveLabel(&d1), // Light -> Auto
		menuKey("ArrowDown"), menuActiveLabel(&d2), // Auto -> Dark
		menuKey("ArrowDown"), menuActiveLabel(&d3), // Dark -> wraps to Light
	); err != nil {
		t.Fatal(err)
	}
	if d1 != "Auto" || d2 != "Dark" {
		t.Fatalf("ArrowDown inside submenu: %q then %q, want Auto then Dark", d1, d2)
	}
	if d3 != "Light" {
		t.Fatalf("ArrowDown from Dark must wrap to Light, not leak into the parent panel: %q", d3)
	}

	// ArrowLeft closes the submenu and returns focus to the parent row.
	if err := chromedp.Run(ctx,
		menuKey("ArrowLeft"),
		chromedp.Sleep(120*time.Millisecond),
		menuSubOpen(&subOpen),
		menuActiveLabel(&label),
		chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um-panel-sub-1"] > summary').getAttribute('aria-expanded'))`, &expanded),
	); err != nil {
		t.Fatal(err)
	}
	if subOpen != "false" || expanded != "false" {
		t.Fatalf("after ArrowLeft: sub open=%s aria-expanded=%s, want false/false", subOpen, expanded)
	}
	if label != "Palette" {
		t.Fatalf("focus after ArrowLeft = %q, want Palette", label)
	}

	// Escape holds at depth: from inside the submenu it closes ONLY the
	// submenu and returns focus to the parent row.
	var inSub, topOpen, afterEsc1 string
	if err := chromedp.Run(ctx,
		menuKey("ArrowRight"),
		chromedp.Sleep(120*time.Millisecond),
		menuKey("ArrowDown"), // focus Auto inside the submenu
		menuActiveLabel(&inSub),
		menuKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		menuSubOpen(&subOpen),
		menuTopOpen(&topOpen),
		menuActiveLabel(&afterEsc1),
	); err != nil {
		t.Fatal(err)
	}
	if inSub != "Auto" {
		t.Fatalf("pre-Escape focus = %q, want Auto", inSub)
	}
	if subOpen != "false" || topOpen != "true" {
		t.Fatalf("first Escape: sub=%s top=%s, want false/true (one level at a time)", subOpen, topOpen)
	}
	if afterEsc1 != "Palette" {
		t.Fatalf("focus after first Escape = %q, want Palette", afterEsc1)
	}

	// The next Escape closes the top menu and refocuses the trigger.
	var focusTrigger string
	if err := chromedp.Run(ctx,
		menuKey("Escape"),
		chromedp.Sleep(150*time.Millisecond),
		menuTopOpen(&topOpen),
		chromedp.Evaluate(`String(document.activeElement && document.activeElement.matches('summary.ui-menu__trigger'))`, &focusTrigger),
	); err != nil {
		t.Fatal(err)
	}
	if topOpen != "false" {
		t.Fatalf("second Escape: top=%s, want false", topOpen)
	}
	if focusTrigger != "true" {
		t.Fatal("focus after second Escape must return to the trigger summary")
	}

	// Tab from inside the submenu closes the whole chain.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(120*time.Millisecond),
		menuKey("ArrowDown"), // Profile -> Palette
		menuKey("ArrowRight"),
		chromedp.Sleep(120*time.Millisecond),
		menuKey("Tab"),
		chromedp.Sleep(150*time.Millisecond),
		menuSubOpen(&subOpen),
		menuTopOpen(&topOpen),
	); err != nil {
		t.Fatal(err)
	}
	if subOpen != "false" || topOpen != "false" {
		t.Fatalf("after Tab: sub=%s top=%s, want false/false (whole chain closes)", subOpen, topOpen)
	}
}

// TestMenuRadioArbitration pins the menuitemradio contract: SSR ships
// aria-checked from MenuItem.Checked, activation (a real click — the
// same event Enter/Space produce on a button row) flips the activated
// row and unchecks its same-group siblings within the panel, and a
// different group in another submenu is untouched.
func TestMenuRadioArbitration(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var label, theme0, theme1, theme2, density string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		// Open the top menu first, then the Palette submenu by clicking
		// its summary: focus can only land inside a VISIBLE subtree, and
		// real usage never opens a submenu with the parent menu closed.
		// The disclosure module's focus-on-open must land on a RADIO row
		// (its selector includes menuitemradio and skips the summary).
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="um-panel-sub-1"] > summary').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuActiveLabel(&label),
		menuCheckedMap(&theme0, "#um-panel-sub-1-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if label != "Light" {
		t.Fatalf("focus after submenu open = %q, want Light (focus-on-open must include menuitemradio)", label)
	}
	if theme0 != `["Light:false","Auto:false","Dark:true"]` {
		t.Fatalf("initial aria-checked map = %s, want Dark checked", theme0)
	}

	if err := chromedp.Run(ctx,
		// Activate Light: Dark must LOSE the check (sibling uncheck is
		// the assertion; a later activation would mask it).
		menuClickRadio("#um-panel-sub-1-panel", "Light"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&theme1, "#um-panel-sub-1-panel"),
		// Activate Auto.
		menuClickRadio("#um-panel-sub-1-panel", "Auto"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&theme2, "#um-panel-sub-1-panel"),
		// Activate Compact in the OTHER group's submenu.
		menuClickRadio("#um-panel-sub-3-panel", "Compact"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&theme2, "#um-panel-sub-1-panel"),
		menuCheckedMap(&density, "#um-panel-sub-3-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if theme1 != `["Light:true","Auto:false","Dark:false"]` {
		t.Fatalf("theme group after activating Light = %s, want Light checked and siblings unchecked", theme1)
	}
	if theme2 != `["Light:false","Auto:true","Dark:false"]` {
		t.Fatalf("theme group after activating Auto and a foreign-group click = %s, want Auto checked and the theme group isolated", theme2)
	}
	if density != `["Cozy:false","Compact:true"]` {
		t.Fatalf("density group after activating Compact = %s, want Compact checked", density)
	}
}

// menuSplitGroupFixture is renderer-captured like menuContractFixture,
// and exists because menuContractFixture structurally cannot express
// the scoping failure: it puts every radio inside a submenu. Here the
// theme group is SPLIT across the boundary — "Light" at the top level,
// Dark / Midnight behind the "More" submenu — plus a contrast group
// confined to the submenu, and a SECOND menu reusing the theme group
// name, which pins that the arbitration scope is one menu, not the
// page.
const menuSplitGroupFixture = `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="split" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="split-panel">Theme<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="split-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Light</span></button><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="split-panel-sub-1"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="split-panel-sub-1-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">More</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="split-panel-sub-1-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="theme"><span class="ui-menu__label">Dark</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Midnight</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="contrast"><span class="ui-menu__label">High</span></button></div></details></div></details><details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="alt" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="alt-panel">Also<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="alt-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="theme"><span class="ui-menu__label">Slate</span></button></div></details>`

// menuGroupCheckedCount counts aria-checked="true" rows of ONE group
// inside a menu root: the number a screen reader experiences as "how
// many options are selected here". A per-row assertion can miss a
// double-check; the count cannot.
func menuGroupCheckedCount(dst *string, menuSel, group string) chromedp.Action {
	return chromedp.Evaluate(fmt.Sprintf(`String(Array.from(
		document.querySelectorAll('%s [data-fui-menu-radio=%q]')
	).filter(r => r.getAttribute('aria-checked') === 'true').length)`, menuSel, group), dst)
}

// TestMenuRadioGroupSpansSubmenus pins the arbitration SCOPE: one
// data-fui-menu-radio group is one group anywhere inside its menu, so
// a group split across the top panel and a submenu keeps exactly one
// checked row in BOTH directions — top-level activation reaches into
// the submenu, and submenu activation reaches back up. The pre-fix
// loop iterated panel.querySelectorAll('[role="menuitemradio"]'), and
// a submenu's panel is a DOM descendant of the parent panel: the
// top-level direction reached into submenus while the submenu
// direction could not reach up, leaving two aria-checked="true" rows
// in one group. The "alt" menu reusing the group name stays untouched:
// the scope is the menu, not the page.
func TestMenuRadioGroupSpansSubmenus(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuSplitGroupFixture)
	ctx := newSeedBrowserCtx(t)

	var count, top, sub, alt string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		// Open the menu, then the More submenu by clicking its
		// summary: both panels visible, the only realistic shape.
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="split"] > summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="split-panel-sub-1"] > summary').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuGroupCheckedCount(&count, `details[data-fui-menu="split"]`, "theme"),
		menuCheckedMap(&top, "#split-panel"),
		menuCheckedMap(&sub, "#split-panel-sub-1-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if count != "1" {
		t.Fatalf("initial checked theme rows = %s, want 1 (Dark)", count)
	}
	// menuCheckedMap is a DESCENDANT query, so the top map covers both
	// levels: Light at the top, Dark/Midnight/High in the submenu.
	if top != `["Light:false","Dark:true","Midnight:false","High:true"]` {
		t.Fatalf("initial state = %s, want Dark checked, Light/Midnight unchecked, contrast High checked", top)
	}

	// Top-level activation reaching DOWN into the submenu.
	if err := chromedp.Run(ctx,
		menuClickRadio("#split-panel", "Light"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&top, "#split-panel"),
		menuCheckedMap(&sub, "#split-panel-sub-1-panel"),
		menuGroupCheckedCount(&count, `details[data-fui-menu="split"]`, "theme"),
		menuCheckedMap(&alt, "#alt-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if top != `["Light:true","Dark:false","Midnight:false","High:true"]` {
		t.Fatalf("after activating Light = %s, want Light checked, Dark/Midnight unchecked (top-level reach into the submenu), contrast untouched", top)
	}
	if sub != `["Dark:false","Midnight:false","High:true"]` {
		t.Fatalf("after activating Light, submenu = %s, want Dark/Midnight unchecked, contrast group untouched", sub)
	}
	if count != "1" {
		t.Fatalf("after activating Light, checked theme rows = %s, want exactly 1", count)
	}

	// Submenu activation reaching back UP — the direction the
	// descendant-scope bug could not see, leaving Light checked.
	if err := chromedp.Run(ctx,
		menuClickRadio("#split-panel-sub-1-panel", "Midnight"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&top, "#split-panel"),
		menuCheckedMap(&sub, "#split-panel-sub-1-panel"),
		menuGroupCheckedCount(&count, `details[data-fui-menu="split"]`, "theme"),
		menuCheckedMap(&alt, "#alt-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if top != `["Light:false","Dark:false","Midnight:true","High:true"]` {
		t.Fatalf("after activating Midnight = %s, want Midnight checked and Light UNchecked (the submenu-to-top direction the descendant-scope bug could not reach)", top)
	}
	if sub != `["Dark:false","Midnight:true","High:true"]` {
		t.Fatalf("after activating Midnight, submenu = %s, want Midnight checked, contrast untouched", sub)
	}
	if count != "1" {
		t.Fatalf("after activating Midnight, checked theme rows = %s, want exactly 1 (this is the screen-reader double-check)", count)
	}
	if alt != `["Slate:true"]` {
		t.Fatalf("after activating Midnight, foreign menu = %s, want untouched", alt)
	}
}

// TestMenuTypeAheadMatchesLabelsOnly: type-ahead matches the
// .ui-menu__label span alone. The radio check and the submenu caret are
// CSS pseudo-elements (never in textContent), but Dark ALSO carries a
// real icon span whose text ("◐") IS in the row's textContent — a
// matcher that reads the whole row sees "◐dark", which neither starts
// with "d" nor should start with "◐". Both directions are pinned:
// typing "d" reaches Dark, and typing the icon glyph itself reaches
// nothing.
func TestMenuTypeAheadMatchesLabelsOnly(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var atLight, afterD, afterIcon string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="um-panel-sub-1"] > summary').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuActiveLabel(&atLight),
		menuKey("d"),
		menuActiveLabel(&afterD),
		// Park focus back on Light, let the 800ms type-ahead buffer
		// expire (otherwise the prefix becomes "d◐"), then type the
		// icon glyph: it must match NO row.
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#um-panel-sub-1-panel [role="menuitemradio"]')).find(r => r.textContent.includes('Light')).focus()`, nil),
		chromedp.Sleep(900*time.Millisecond),
		menuKey("◐"),
		menuActiveLabel(&afterIcon),
	); err != nil {
		t.Fatal(err)
	}
	if atLight != "Light" {
		t.Fatalf("focus after opening Palette = %q, want Light", atLight)
	}
	if afterD != "Dark" {
		t.Fatalf("type-ahead 'd' = %q, want Dark (icon text must not hide the label: '◐dark' does not start with 'd')", afterD)
	}
	if afterIcon != "Light" {
		t.Fatalf("type-ahead '◐' = %q, want focus unchanged on Light (the icon glyph is not part of any label)", afterIcon)
	}
}

// TestMenuDisabledParentUnreachable: an aria-disabled submenu parent is
// skipped by roving focus (End lands on the last ENABLED row) and
// ArrowRight on it does not open its submenu.
func TestMenuDisabledParentUnreachable(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var atEnd, lockedOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(150*time.Millisecond),
		// End must land on Density (the disabled Locked row is filtered
		// out of the rotation), never on Locked.
		menuKey("End"),
		menuActiveLabel(&atEnd),
		// Even with focus forced onto the disabled row, ArrowRight must
		// not open its submenu.
		chromedp.Evaluate(`Array.from(document.querySelectorAll('#um-panel > details > summary')).find(s => s.textContent.includes('Locked')).focus()`, nil),
		menuKey("ArrowRight"),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="um-panel-sub-4"]').open)`, &lockedOpen),
	); err != nil {
		t.Fatal(err)
	}
	if atEnd != "Density" {
		t.Fatalf("End landed on %q, want Density (disabled parent must be skipped)", atEnd)
	}
	if lockedOpen != "false" {
		t.Fatalf("ArrowRight on the disabled parent opened its submenu (%s), want false", lockedOpen)
	}
}

// menuSubFirstFixture carries the two shapes the focus-on-open selector
// regressed on, both captured from the component renderer like
// menuContractFixture:
//
//   - "sf": the panel's FIRST row is itself a submenu parent (SubFirst),
//     then a plain row. A descendant search for the first plain
//     [role=menuitem] matches A1 first, hidden inside the still-closed
//     nested <details>, and .focus() on a hidden row is a silent no-op:
//     the menu opens with focus on the trigger summary, which carries no
//     role, so ArrowDown / Home / End / type-ahead are all dead.
//   - "nest": a parent whose panel's first row is another parent
//     (Outer -> Inner -> Leaf). Keyboard entry masks this one (menu.js
//     focuses the first row synchronously before the async toggle
//     refocus), so both menus here are opened by CLICK.
const menuSubFirstFixture = `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="sf" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="sf-panel">Menu<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="sf-panel" role="menu" data-fui-menu-panel><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="sf-panel-sub-0"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="sf-panel-sub-0-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">SubFirst</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="sf-panel-sub-0-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">A1</span></button><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">A2</span></button></div></details><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Plain</span></button></div></details><details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="nest" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="nest-panel">Deep<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="nest-panel" role="menu" data-fui-menu-panel><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="nest-panel-sub-0"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="nest-panel-sub-0-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Outer</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="nest-panel-sub-0-panel" role="menu" data-fui-menu-panel><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="nest-panel-sub-0-panel-sub-0"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="nest-panel-sub-0-panel-sub-0-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Inner</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="nest-panel-sub-0-panel-sub-0-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Leaf</span></button></div></details><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Tail</span></button></div></details></div></details>`

// TestMenuFocusOnOpenFirstRowIsSubmenu pins the focus-on-open contract
// for panels whose first row is itself a submenu parent: opening must
// focus THAT row (a <summary role=menuitem> in the panel's own scope),
// never a row hidden inside the still-closed nested <details>. When the
// search is not scoped to the panel, the hidden row wins document order,
// .focus() no-ops, and the menu opens keyboard-dead.
func TestMenuFocusOnOpenFirstRowIsSubmenu(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuSubFirstFixture)
	ctx := newSeedBrowserCtx(t)

	var afterOpen, afterDown, nestedAfterOpen, afterSubOpen string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),

		// Click-open the top menu: focus must land on SubFirst, the
		// panel's own first row, which is a submenu parent summary.
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="sf"] > summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuActiveLabel(&afterOpen),
		// The open menu must be keyboard-live immediately: ArrowDown
		// from SubFirst goes to Plain, not into the closed submenu.
		menuKey("ArrowDown"),
		menuActiveLabel(&afterDown),

		// The nested shape: opening the nest root lands on Outer (the
		// root panel's first row is a parent, the sf shape one level
		// down), and CLICK-opening Outer's submenu must then focus
		// Inner — Outer's panel has another parent as ITS first row.
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="nest"] > summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuActiveLabel(&nestedAfterOpen),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="nest-panel-sub-0"] > summary').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuActiveLabel(&afterSubOpen),
	); err != nil {
		t.Fatal(err)
	}
	if afterOpen != "SubFirst" {
		t.Fatalf("focus after open = %q, want SubFirst (the panel's own first row; a hidden row inside the closed submenu means focus landed nowhere)", afterOpen)
	}
	if afterDown != "Plain" {
		t.Fatalf("ArrowDown after open = %q, want Plain (the menu must answer the keyboard the moment it opens)", afterDown)
	}
	if nestedAfterOpen != "Outer" {
		t.Fatalf("focus after opening nest = %q, want Outer (the root panel's own first row)", nestedAfterOpen)
	}
	if afterSubOpen != "Inner" {
		t.Fatalf("focus after click-opening Outer = %q, want Inner (a parent whose panel's first row is another parent)", afterSubOpen)
	}
}

// TestMenuSubmenuArrowKeysSwapInRTL: in RTL the open/close arrows swap
// — ArrowLeft opens a submenu and moves focus in, ArrowRight closes it
// and returns focus to the parent row — because menu.js reads the
// row's computed direction per keypress. dir="rtl" is set on the menu
// root after load, no reload needed.
func TestMenuSubmenuArrowKeysSwapInRTL(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var atPalette, subAfterLeft, labelAfterLeft, subAfterRight, labelAfterRight string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="um"]').setAttribute('dir','rtl')`, nil),
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuKey("ArrowDown"), // Profile -> Palette
		menuActiveLabel(&atPalette),
		// ArrowLeft is the OPEN key in RTL.
		menuKey("ArrowLeft"),
		chromedp.Sleep(120*time.Millisecond),
		menuSubOpen(&subAfterLeft),
		menuActiveLabel(&labelAfterLeft),
		// ArrowRight is the CLOSE key in RTL.
		menuKey("ArrowRight"),
		chromedp.Sleep(120*time.Millisecond),
		menuSubOpen(&subAfterRight),
		menuActiveLabel(&labelAfterRight),
	); err != nil {
		t.Fatal(err)
	}
	if atPalette != "Palette" {
		t.Fatalf("ArrowDown after open = %q, want Palette", atPalette)
	}
	if subAfterLeft != "true" || labelAfterLeft != "Light" {
		t.Fatalf("ArrowLeft in RTL: sub=%s focus=%q, want open and Light (ArrowLeft is the open key in RTL)", subAfterLeft, labelAfterLeft)
	}
	if subAfterRight != "false" || labelAfterRight != "Palette" {
		t.Fatalf("ArrowRight in RTL: sub=%s focus=%q, want closed and Palette (ArrowRight is the close key in RTL)", subAfterRight, labelAfterRight)
	}
}

// TestMenuHomeJumpsToFirstRow: Home lands on the first enabled row of
// the own panel. End is pinned by TestMenuDisabledParentUnreachable;
// Home had no coverage at all.
func TestMenuHomeJumpsToFirstRow(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var atEnd, afterHome string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(menuOpenTop, nil),
		chromedp.Sleep(150*time.Millisecond),
		menuKey("End"),
		menuActiveLabel(&atEnd),
		menuKey("Home"),
		menuActiveLabel(&afterHome),
	); err != nil {
		t.Fatal(err)
	}
	if atEnd != "Density" {
		t.Fatalf("End = %q, want Density", atEnd)
	}
	if afterHome != "Profile" {
		t.Fatalf("Home = %q, want Profile (first enabled row of the panel)", afterHome)
	}
}

// menuUngroupedRadioFixture is HAND-WRITTEN, unlike the renderer-captured
// fixtures above: framework/ui cannot emit a menuitemradio without
// data-fui-menu-radio (an empty Radio renders the plain menuitem), but
// the runtime contract covers hand-authored markup too, and menu.js
// promises that rows without a group key "form an implicit group of one
// (the row still self-checks)". One panel, two shapes: a g1 pair (A
// checked) and an ungrouped row U.
const menuUngroupedRadioFixture = `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="ug"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="ug-panel">Pick<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="ug-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="g1"><span class="ui-menu__label">A</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="g1"><span class="ui-menu__label">B</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false"><span class="ui-menu__label">U</span></button></div></details>`

// TestMenuUngroupedRadioSelfChecks: an ungrouped menuitemradio is an
// implicit group of one — activating it checks it and touches nobody
// else, and activating a grouped row never unchecks it. The natural
// buggy reading of the arbitration loop (group === null matches EVERY
// radio in the panel) wipes the checked state of real groups.
func TestMenuUngroupedRadioSelfChecks(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuUngroupedRadioFixture)
	ctx := newSeedBrowserCtx(t)

	var afterU, afterB string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(700*time.Millisecond),
		chromedp.Evaluate(`document.querySelector('details[data-fui-menu="ug"] > summary.ui-menu__trigger').click()`, nil),
		chromedp.Sleep(150*time.Millisecond),
		// Activate U (no data-fui-menu-radio): U self-checks, the g1
		// pair keeps its own checked row.
		menuClickRadio("#ug-panel", "U"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&afterU, "#ug-panel"),
		// Activate B: B takes the g1 check, U keeps its own.
		menuClickRadio("#ug-panel", "B"),
		chromedp.Sleep(100*time.Millisecond),
		menuCheckedMap(&afterB, "#ug-panel"),
	); err != nil {
		t.Fatal(err)
	}
	if afterU != `["A:true","B:false","U:true"]` {
		t.Fatalf("after activating ungrouped U = %s, want U checked and the g1 pair untouched (implicit group of one)", afterU)
	}
	if afterB != `["A:false","B:true","U:true"]` {
		t.Fatalf("after activating grouped B = %s, want B checked and U keeping its own check", afterB)
	}
}

// TestMenuButtonTriggerRealClick: a TriggerHTML button inside the
// summary must toggle the disclosure under a REAL pointer. Chrome's UA
// activation does not run when the click target is an interactive
// descendant, so without the disclosure module's interactive-descendant
// toggle the menu opens dead. (The closed-panel CSS hiding is pinned
// markup-side in framework/ui/menu_test.go: menuCSS must not defeat
// the UA's closed-details display with its author display:grid.)
func TestMenuButtonTriggerRealClick(t *testing.T) {
	g := startGadgetServer(t, `[]`, `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="bm" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="bm-panel"><button type="button" aria-label="Open user menu"><span>U</span></button></summary><div class="ui-menu__panel" id="bm-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/x" role="menuitem" tabindex="-1"><span class="ui-menu__label">Row</span></a></div></details>`)
	ctx := newSeedBrowserCtx(t)

	var coords string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`summary.ui-menu__trigger > button`),
		chromedp.Evaluate(`(() => { const r = document.querySelector('summary.ui-menu__trigger > button').getBoundingClientRect(); return r.x + r.width/2 + ',' + (r.y + r.height/2); })()`, &coords),
	); err != nil {
		t.Fatal(err)
	}
	var x, y float64
	if _, err := fmt.Sscanf(coords, "%f,%f", &x, &y); err != nil {
		t.Fatalf("coords: %v (%q)", err, coords)
	}
	var after string
	if err := chromedp.Run(ctx,
		chromedp.MouseClickXY(x, y),
		chromedp.Evaluate(`String(document.querySelector('details[data-fui-menu="bm"]').open)`, &after),
	); err != nil {
		t.Fatal(err)
	}
	if after != "true" {
		t.Fatal("real click on TriggerHTML button must open the disclosure")
	}
}
