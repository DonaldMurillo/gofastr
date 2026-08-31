package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// menuContractFixture is the exact SSR of a framework/ui.Menu carrying
// every shape this contract covers: plain rows, a Palette submenu of
// menuitemradio rows (theme group, Dark checked), a separator, a second
// radio group in its own submenu (density), and a Disabled submenu
// parent whose children must stay unreachable. Captured from the
// component renderer so the fixture cannot drift from production
// markup (same discipline as the tabs contract fixture).
const menuContractFixture = `<details class="ui-menu ui-menu--bottom-start" data-fui-disclosure data-fui-menu="um" data-fui-comp="ui-menu"><summary class="ui-menu__trigger" aria-haspopup="menu" aria-controls="um-panel">Account<span class="ui-menu__caret" aria-hidden="true">▾</span></summary><div class="ui-menu__panel" id="um-panel" role="menu" data-fui-menu-panel><a class="ui-menu__item" href="/me" role="menuitem" tabindex="-1"><span class="ui-menu__label">Profile</span></a><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-1"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="um-panel-sub-1-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Palette</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-1-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Light</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="theme"><span class="ui-menu__label">Auto</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="theme"><span class="ui-menu__label">Dark</span></button></div></details><hr class="ui-menu__sep" role="separator"><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-3"><summary class="ui-menu__item ui-menu__item--hassub" aria-haspopup="menu" aria-controls="um-panel-sub-3-panel" role="menuitem" tabindex="-1"><span class="ui-menu__label">Density</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-3-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="true" data-fui-menu-radio="density"><span class="ui-menu__label">Cozy</span></button><button class="ui-menu__item" type="button" role="menuitemradio" tabindex="-1" aria-checked="false" data-fui-menu-radio="density"><span class="ui-menu__label">Compact</span></button></div></details><details class="ui-menu__sub" data-fui-disclosure data-fui-menu="um-panel-sub-4"><summary class="ui-menu__item ui-menu__item--hassub ui-menu__item--disabled" aria-haspopup="menu" aria-controls="um-panel-sub-4-panel" role="menuitem" tabindex="-1" aria-disabled="true"><span class="ui-menu__label">Locked</span></summary><div class="ui-menu__panel ui-menu__panel--sub" id="um-panel-sub-4-panel" role="menu" data-fui-menu-panel><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Hidden1</span></button><button class="ui-menu__item" type="button" role="menuitem" tabindex="-1"><span class="ui-menu__label">Hidden2</span></button></div></details></div></details>`

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

// TestMenuTypeAheadMatchesLabelsOnly: the radio check and the submenu
// caret are CSS pseudo-elements, so type-ahead sees the label alone —
// typing "d" from Light jumps to Dark.
func TestMenuTypeAheadMatchesLabelsOnly(t *testing.T) {
	g := startGadgetServer(t, `[]`, menuContractFixture)
	ctx := newSeedBrowserCtx(t)

	var atLight, afterD string
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
	); err != nil {
		t.Fatal(err)
	}
	if atLight != "Light" {
		t.Fatalf("focus after opening Palette = %q, want Light", atLight)
	}
	if afterD != "Dark" {
		t.Fatalf("type-ahead 'd' = %q, want Dark (pseudo-element content must not pollute the label)", afterD)
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
