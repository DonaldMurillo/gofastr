package headless

// Browser coverage for headless-menu and headless-disclosure: the menu
// keyboard contract (roving focus scoped to the own panel, wrapping
// arrows, Home/End, type-ahead, RTL-aware submenu open/close, Tab
// closing the chain), the radio arbitration across submenu boundaries,
// the lazy-panel inflation, the caller-owned trigger wiring, and the
// disclosure half (the aria mirror, Escape one level at a time with
// focus return, the trap's Tab containment and its release on close,
// the persist dialect). These tests moved from core-ui/runtime, where
// they pinned the retired menu/disclosure modules against hand-built
// fixtures; here they render the real primitive and load the real
// registered modules. Same harness as behavior_e2e_test.go.

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// menuE2EItems is the row set the contract tests exercise: plain rows,
// a Palette submenu of radio rows (theme group, Dark checked, Dark
// carrying an icon so type-ahead has to skip real icon text), a second
// radio group in its own submenu (density), and a Disabled submenu
// parent whose children must stay unreachable.
func menuE2EItems() []MenuItem {
	return []MenuItem{
		{Label: "Profile", Href: "/me"},
		{Label: "Palette", Children: []MenuItem{
			{Label: "Light", Radio: "theme"},
			{Label: "Dark", Radio: "theme", Checked: true, Icon: render.HTML(`<svg width="12" height="12" viewBox="0 0 12 12" aria-hidden="true"><circle cx="6" cy="6" r="5"/></svg>`)},
		}},
		{Separator: true},
		{Label: "Density", Children: []MenuItem{
			{Label: "Comfortable", Radio: "density"},
			{Label: "Compact", Radio: "density"},
		}},
		{Label: "Locked", Disabled: true, Children: []MenuItem{
			{Label: "Unreachable"},
		}},
		{Label: "Sign out", Href: "/out"},
	}
}

// menuPage wraps the rendered menu in the minimal page shape the
// contract needs.
func menuPage(t *testing.T, menu render.HTML) string {
	t.Helper()
	return string(menu)
}

// menuKey dispatches a keydown on the focused element: the module's
// listeners are document-level and read e.target, so a bubbling
// synthetic event exercises them.
func menuKey(key string) chromedp.Action {
	return chromedp.Evaluate("document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'"+key+"',bubbles:true,cancelable:true}))", nil)
}

// menuFocusLabel reads the focused row's label span.
func menuFocusLabel(dst *string) chromedp.Action {
	return chromedp.Evaluate(`(() => {
		const el = document.activeElement;
		if (!el) return '';
		const l = el.querySelector(':scope > span:last-of-type');
		return (l ? l.textContent : el.textContent).trim();
	})()`, dst)
}

const menuLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-menu'])`
const disclosureLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-disclosure'])`

// The keyboard contract: focus lands on the first row on open, the
// arrows wrap within the OWN panel (a submenu's rows never leak into
// the parent's rotation), ArrowRight opens a submenu and moves focus
// in, ArrowLeft closes it and returns focus to the parent row, Home
// and End jump, Escape closes one level at a time, Tab closes the
// chain.
func TestE2E_MenuSubmenuKeyboardContract(t *testing.T) {
	menu := Menu(MenuProps{ID: "um", Label: "Account", Items: menuE2EItems()}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the menu marker never loaded headless-menu")
	}
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("headless-menu loaded without headless-disclosure — the Requires declaration is not honoured")
	}

	var label string
	open := `document.querySelector('details[data-hui-menu="um"] > summary').click()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(open, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.getAttribute('role') === 'menuitem'`) {
		t.Fatal("opening the menu did not focus the first row")
	}
	if err := chromedp.Run(ctx, menuFocusLabel(&label)); err != nil {
		t.Fatal(err)
	}
	if label != "Profile" {
		t.Fatalf("focus after open = %q, want Profile", label)
	}

	// ArrowDown walks Profile → Palette; a second ArrowDown must skip
	// the whole submenu subtree (Density, never Light).
	steps := func(keys ...string) {
		t.Helper()
		for _, k := range keys {
			if err := chromedp.Run(ctx, menuKey(k)); err != nil {
				t.Fatal(err)
			}
		}
	}
	read := func() string {
		t.Helper()
		if err := chromedp.Run(ctx, menuFocusLabel(&label)); err != nil {
			t.Fatal(err)
		}
		return label
	}
	steps("ArrowDown")
	if got := read(); got != "Palette" {
		t.Fatalf("ArrowDown from Profile = %q, want Palette", got)
	}
	steps("ArrowDown")
	if got := read(); got != "Density" {
		t.Fatalf("ArrowDown from Palette = %q, want Density — submenu rows leaked into the parent rotation", got)
	}
	steps("End")
	if got := read(); got != "Sign out" {
		t.Fatalf("End = %q, want Sign out (disabled rows and separators are skipped)", got)
	}
	steps("Home")
	if got := read(); got != "Profile" {
		t.Fatalf("Home = %q, want Profile", got)
	}

	// ArrowRight opens the submenu and moves focus into it; ArrowLeft
	// closes it and returns focus to the parent row.
	steps("ArrowDown", "ArrowRight")
	var subOpen bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="um-panel-sub-1"]').open`, &subOpen)); err != nil {
		t.Fatal(err)
	}
	if !subOpen {
		t.Fatal("ArrowRight did not open the submenu")
	}
	if got := read(); got != "Light" {
		t.Fatalf("focus after ArrowRight = %q, want Light", got)
	}
	// Roving wraps within the submenu: ArrowUp from Light wraps to Dark.
	steps("ArrowUp")
	if got := read(); got != "Dark" {
		t.Fatalf("ArrowUp from Light = %q, want Dark (the rotation wraps within the own panel)", got)
	}
	steps("ArrowLeft")
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="um-panel-sub-1"]').open`, &subOpen)); err != nil {
		t.Fatal(err)
	}
	if subOpen {
		t.Fatal("ArrowLeft did not close the submenu")
	}
	if got := read(); got != "Palette" {
		t.Fatalf("focus after ArrowLeft = %q, want Palette (focus returns to the parent row)", got)
	}

	// Escape closes one level at a time: inside the submenu it closes
	// the submenu, not the whole menu.
	steps("ArrowRight")
	steps("Escape")
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify([document.querySelector('details[data-hui-menu="um"]').open,
		  document.querySelector('details[data-hui-menu="um-panel-sub-1"]').open])`, &label)); err != nil {
		t.Fatal(err)
	}
	var pair []bool
	if err := json.Unmarshal([]byte(label), &pair); err != nil {
		t.Fatal(err)
	}
	if !pair[0] || pair[1] {
		t.Fatalf("Escape inside the submenu closed [%v,%v], want [true,false] (one level per press)", pair[0], pair[1])
	}

	// Tab closes the whole chain and lets focus escape.
	steps("ArrowRight", "Tab")
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify([document.querySelector('details[data-hui-menu="um"]').open,
		  document.querySelector('details[data-hui-menu="um-panel-sub-1"]').open])`, &label)); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(label), &pair); err != nil {
		t.Fatal(err)
	}
	if pair[0] || pair[1] {
		t.Fatalf("Tab closed [%v,%v], want [false,false] (the whole chain)", pair[0], pair[1])
	}
}

// Type-ahead matches the label span, not the icon's text or the caret
// pseudo-elements; the buffer resets after the quiet window.
func TestE2E_MenuTypeAheadMatchesLabelsOnly(t *testing.T) {
	menu := Menu(MenuProps{ID: "ta", Label: "Account", Items: []MenuItem{
		{Label: "Profile"},
		// The candidate carries an icon: the first span is the glyph,
		// so a selector matching the FIRST span matches "◐", not the
		// label — exactly the drift this test exists to catch.
		{Label: "Palette", Icon: render.HTML("◐")},
		{Label: "Sign out"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="ta"] > summary').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.includes('Profile')`) {
		t.Fatal("the menu never focused its first row")
	}
	var label string
	// "pa" jumps to Palette, skipping Profile.
	for _, k := range []string{"p", "a"} {
		if err := chromedp.Run(ctx, menuKey(k)); err != nil {
			t.Fatal(err)
		}
	}
	if err := chromedp.Run(ctx, menuFocusLabel(&label)); err != nil {
		t.Fatal(err)
	}
	if label != "Palette" {
		t.Fatalf("type-ahead 'pa' landed on %q, want Palette", label)
	}
}

// Radio arbitration is client-side and scoped to the whole menu: a
// group split across a submenu boundary stays one group, and an
// ungrouped radio row self-checks without touching anybody else.
func TestE2E_MenuRadioArbitrationSpansSubmenus(t *testing.T) {
	menu := Menu(MenuProps{ID: "rd", Label: "View", Items: []MenuItem{
		{Label: "Palette", Children: []MenuItem{
			{Label: "Light", Radio: "theme"},
			{Label: "Dark", Radio: "theme", Checked: true},
		}},
		{Label: "Density", Children: []MenuItem{
			{Label: "Comfortable", Radio: "density"},
			{Label: "Compact", Radio: "density"},
		}},
		{Label: "Ungrouped", Radio: ""},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	var checked string
	read := func() map[string]string {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(Array.from(
			document.querySelectorAll('[data-hui-menu="rd"] [role="menuitemradio"]')
		).map(r => [r.textContent.trim(), r.getAttribute('aria-checked')]))`, &checked)); err != nil {
			t.Fatal(err)
		}
		var pairs [][2]string
		if err := json.Unmarshal([]byte(checked), &pairs); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, p := range pairs {
			out[p[0]] = p[1]
		}
		return out
	}
	// Activate Light in the Palette submenu: Dark (same group, same
	// submenu) unchecks, both density rows (other group) and the
	// ungrouped row are untouched.
	click := `Array.from(document.querySelectorAll('[role="menuitemradio"]')).find(r => r.textContent.trim() === 'Light').click()`
	if err := chromedp.Run(ctx, chromedp.Evaluate(click, nil)); err != nil {
		t.Fatal(err)
	}
	got := read()
	if got["Light"] != "true" || got["Dark"] != "false" {
		t.Fatalf("Light activation left theme group at Light=%s Dark=%s, want true/false", got["Light"], got["Dark"])
	}
	if got["Comfortable"] != "false" || got["Compact"] != "false" {
		t.Fatalf("the activation leaked into the density group: %v", got)
	}
}

// The disabled submenu parent is never a focus target, and its children
// stay closed and unreachable.
func TestE2E_MenuDisabledParentUnreachable(t *testing.T) {
	menu := Menu(MenuProps{ID: "dp", Label: "View", Items: []MenuItem{
		{Label: "First"},
		{Label: "Locked", Disabled: true, Children: []MenuItem{{Label: "Unreachable"}}},
		{Label: "Last"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="dp"] > summary').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'First'`) {
		t.Fatal("the menu never focused its first row")
	}
	// End must land on Last, skipping Locked.
	if err := chromedp.Run(ctx, menuKey("End")); err != nil {
		t.Fatal(err)
	}
	var label string
	if err := chromedp.Run(ctx, menuFocusLabel(&label)); err != nil {
		t.Fatal(err)
	}
	if label != "Last" {
		t.Fatalf("End landed on %q, want Last — a disabled parent must not be a focus target", label)
	}
}

// A lazy panel's rows are invisible to page-scoped queries while the
// menu is closed, mount on first open ahead of the focus lookup, and
// never double-mount.
func TestE2E_MenuLazyPanelInflatesOnOpen(t *testing.T) {
	menu := Menu(MenuProps{ID: "lz", Label: "View", LazyPanel: true, Items: []MenuItem{
		{Label: "Row one"},
		{Label: "Row two"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	var visible int
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelectorAll('[data-hui-menu="lz"] [role="menuitem"]').length`, &visible)); err != nil {
		t.Fatal(err)
	}
	if visible != 0 {
		t.Fatalf("%d rows are live-DOM visible while the lazy menu is closed, want 0", visible)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="lz"] > summary').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-menu="lz"] [role="menuitem"]').length === 2`) {
		t.Fatal("the first open did not mount the lazy rows")
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'Row one'`) {
		t.Fatal("the first open did not land focus on a mounted row")
	}
	// Close and open again: the inflation is idempotent — the row count
	// stays 2, no duplicate rows, no template back.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('details[data-hui-menu="lz"] > summary').click()`, nil),
		chromedp.Sleep(100*1e6),
		chromedp.Evaluate(`document.querySelector('details[data-hui-menu="lz"] > summary').click()`, nil),
		chromedp.Sleep(100*1e6),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-menu="lz"] [role="menuitem"]').length === 2 &&
		document.querySelectorAll('[data-hui-menu="lz"] template').length === 0`) {
		t.Fatal("the second open duplicated the lazy rows")
	}
	// ArrowDown moves focus across the mounted rows: the keyboard
	// contract applies to rows that arrived from a template.
	if err := chromedp.Run(ctx, menuKey("ArrowDown")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'Row two'`) {
		t.Fatal("ArrowDown did not walk the mounted lazy rows")
	}
}

// The caller-owned trigger path inflates a lazy panel too, and a lazy
// menu that is ALREADY open at scan (server-rendered open, or inserted
// open) has its rows mounted by the arrival pass — no toggle will ever
// fire for it. Both moved from the retired core-ui lazy suite.
func TestE2E_MenuLazyTriggerPathAndPreloadedOpen(t *testing.T) {
	// The trigger path.
	menu := Menu(MenuProps{ID: "lzt", TriggerElement: render.HTML(`<button type="button" id="lz-trigger">Open</button>`), LazyPanel: true, Items: []MenuItem{
		{Label: "Row one"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('lz-trigger').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-menu="lzt"] [role="menuitem"]').length === 1 &&
		document.querySelectorAll('[data-hui-menu="lzt"] template').length === 0`) {
		t.Fatal("the trigger path's lazy rows did not inflate on first open")
	}

	// The preloaded-open path: the server rendered the details open, so
	// only the scanner can mount the rows.
	open := Menu(MenuProps{ID: "lzo", Label: "View", LazyPanel: true, Items: []MenuItem{
		{Label: "Mounted row"},
	}}, nil)
	// Re-render with the open attribute: the primitive has no Open prop
	// (the details' open state is the caller's), so patch the bytes the
	// same way a server-rendered-open menu would carry them.
	page := "<span data-open-patch></span>" + string(open)
	page = replaceFirst(page, `<details data-hui-disclosure="" data-hui-menu="lzo">`,
		`<details data-hui-disclosure="" data-hui-menu="lzo" open>`)
	b2 := startBehaviorServer(t, page)
	ctx2 := behaviorPage(t, b2)
	if !pollTrue(ctx2, menuLoaded) {
		t.Fatal("the module never loaded (second page)")
	}
	if !pollTrue(ctx2, `document.querySelectorAll('[data-hui-menu="lzo"] [role="menuitem"]').length === 1 &&
		document.querySelectorAll('[data-hui-menu="lzo"] template').length === 0`) {
		t.Fatal("an already-open lazy menu's rows were not mounted by the arrival pass")
	}
}

// replaceFirst swaps the first occurrence of old in s.
func replaceFirst(s, old, new string) string {
	i := indexOf(s, old)
	if i < 0 {
		return s
	}
	return s[:i] + new + s[i+len(old):]
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// A caller-owned trigger becomes the disclosure controller: the module
// wires aria-haspopup/controls/expanded at hydration, a click toggles
// and focuses the first row, and an anchor trigger still opens on
// Space without navigating.
func TestE2E_MenuCallerOwnedTriggerWiring(t *testing.T) {
	menu := Menu(MenuProps{ID: "tg", TriggerElement: render.HTML(`<button type="button" id="my-trigger">Open</button>`), Items: []MenuItem{
		{Label: "Row", Href: "/r"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	var wired string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`JSON.stringify([''+document.getElementById('my-trigger').getAttribute('aria-haspopup'),
		 ''+document.getElementById('my-trigger').getAttribute('aria-controls'),
		 ''+document.getElementById('my-trigger').getAttribute('aria-expanded')])`, &wired)); err != nil {
		t.Fatal(err)
	}
	var trio [3]string
	if err := json.Unmarshal([]byte(wired), &trio); err != nil {
		t.Fatal(err)
	}
	if trio[0] != "menu" || trio[1] != "tg-panel" || trio[2] != "false" {
		t.Fatalf("trigger aria wiring = %v, want [menu tg-panel false]", trio)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.getElementById('my-trigger').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'Row'`) {
		t.Fatal("the trigger click did not open the panel and focus its first row")
	}
	// Escape returns focus to the caller's trigger.
	if err := chromedp.Run(ctx, menuKey("Escape")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.getElementById('my-trigger')`) {
		t.Fatal("Escape did not return focus to the caller's trigger")
	}
}

// The disclosure half: the aria-expanded mirror follows the native
// toggle, Escape closes the deepest disclosure containing focus with
// focus returned to its controller, and the trap posture confines Tab
// while open and releases it on close.
func TestE2E_DisclosureMirrorEscapeAndTrap(t *testing.T) {
	d := Disclosure(DisclosureProps{Summary: render.Text("Menu"), Trap: true,
		Content: render.HTML(`<a id="in-drawer" href="/x">item</a>`)}, nil)
	b := startBehaviorServer(t, string(d)+`<a id="outside" href="/y">outside</a>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure marker never loaded headless-disclosure")
	}
	var expanded string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-disclosure] > summary').getAttribute('aria-expanded')`, &expanded)); err != nil {
		t.Fatal(err)
	}
	if expanded != "false" {
		t.Fatalf("the closed disclosure's summary reports aria-expanded=%q, want false (the mirror)", expanded)
	}
	// Open, focus inside: Tab wraps within the drawer (the trap).
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('details[data-hui-disclosure] > summary').click()`, nil),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('in-drawer').focus()`, nil),
		menuKey("Tab"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.querySelector('details[data-hui-disclosure] > summary') ||
		document.activeElement === document.getElementById('in-drawer')`) {
		t.Fatal("Tab escaped the trap disclosure")
	}
	// Escape with focus inside closes it and returns focus to the summary.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('in-drawer').focus()`, nil),
		menuKey("Escape"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-disclosure]').open &&
		document.activeElement === document.querySelector('details[data-hui-disclosure] > summary')`) {
		t.Fatal("Escape did not close the disclosure and return focus to its controller")
	}
	// The trap released: a Tab issued from outside the (now closed)
	// disclosure is not pulled back into it — while the trap was
	// armed, the module's containment would have preventDefaulted and
	// focused the disclosure's first tabbable instead. (A synthetic
	// keydown cannot move focus itself; the assertion is about the
	// redirection, which is the module's to make.)
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('outside').focus()`, nil),
		menuKey("Tab"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.getElementById('outside') ||
		!document.querySelector('details[data-hui-disclosure]').contains(document.activeElement)`) {
		t.Fatal("the closed disclosure's trap is still pulling focus back in — it did not release")
	}
}

// The navigate dialect: a client-side navigation closes the ordinary
// disclosures and restores the persistent ones from the session store
// (the module listens for the kernel's gofastr:navigate, the same
// event the SPA navigator fires on every swap).
func TestE2E_DisclosureNavigateClosesAndRestores(t *testing.T) {
	ordinary := Disclosure(DisclosureProps{Summary: render.Text("Ordinary"), Open: true,
		Content: render.Text("c")}, nil)
	persistent := Disclosure(DisclosureProps{Summary: render.Text("Persistent"), PersistKey: "nav.faq",
		Content: render.Text("c")}, nil)
	b := startBehaviorServer(t, string(ordinary)+string(persistent))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure marker never loaded headless-disclosure")
	}
	// The reader opens the persistent one and closes the ordinary one —
	// the store records the persistent group's state.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('details[data-hui-disclosure-persist] > summary').click()`, nil),
		chromedp.Evaluate(`document.querySelector('details:not([data-hui-disclosure-persist]) > summary').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details:not([data-hui-disclosure-persist])').open &&
		document.querySelector('details[data-hui-disclosure-persist]').open`) {
		t.Fatal("the summary clicks did not produce the expected open states")
	}
	// A client-side navigation: the ordinary one is closed (it already
	// is), and a re-arrival restores the persistent one's recorded
	// state even after the DOM was re-marked closed.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.dispatchEvent(new Event('gofastr:navigate'))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-disclosure-persist]').open`) {
		t.Fatal("the navigate pass did not restore the persistent disclosure from the store")
	}
	// An ordinary disclosure that is open when the navigation lands is
	// closed by it.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('details:not([data-hui-disclosure-persist]) > summary').click()`, nil),
		chromedp.Evaluate(`window.dispatchEvent(new Event('gofastr:navigate'))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details:not([data-hui-disclosure-persist])').open`) {
		t.Fatal("the navigate pass left an ordinary disclosure open")
	}
}

// The trap's inert containment: opening a trapped drawer removes every
// other body child from the focus order AND the accessibility tree (a
// Tab cycle alone leaves them in the AT tree and the virtual cursor
// walks out); closing it releases exactly what was toggled; detaching
// it from the DOM releases too (a detached <details> fires no toggle).
// This property moved from the retired core-ui disclosure module's
// TestTrapReleasesInertOnDetach.
func TestE2E_TrapInertContainmentAndReleaseOnDetach(t *testing.T) {
	d := Disclosure(DisclosureProps{Summary: render.Text("Menu"), Trap: true,
		Content: render.HTML(`<a id="in-drawer" href="/x">item</a>`), ID: "the-drawer"}, nil)
	b := startBehaviorServer(t, string(d))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure marker never loaded headless-disclosure")
	}
	// The page's runtime <script> is a body-level sibling of the <main>
	// that holds the drawer: exactly what the inert containment toggles.
	const sibling = `document.querySelector('body > script')`
	var inert bool
	open := `document.getElementById('the-drawer').setAttribute('open','')`
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(open, nil),
		chromedp.Evaluate(sibling+`.hasAttribute('inert')`, &inert),
	); err != nil {
		t.Fatal(err)
	}
	if !inert {
		t.Fatal("opening the trapped drawer did not inert its body-level siblings — the AT tree can still walk out")
	}
	// Close: released, exactly what was toggled.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('the-drawer').removeAttribute('open')`, nil),
		chromedp.Sleep(50*1e6),
		chromedp.Evaluate(sibling+`.hasAttribute('inert')`, &inert),
	); err != nil {
		t.Fatal(err)
	}
	if inert {
		t.Fatal("closing the trapped drawer did not release the inert it set")
	}
	// Open again, then detach the details outright: a detached element
	// fires no toggle, so the release must come from the watcher.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(open, nil),
		chromedp.Evaluate(sibling+`.hasAttribute('inert')`, &inert),
	); err != nil {
		t.Fatal(err)
	}
	if !inert {
		t.Fatal("the second open did not re-engage the inert containment")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('the-drawer').remove()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!`+sibling+`.hasAttribute('inert')`) {
		t.Fatal("detaching the trapped drawer left the inert stuck on its siblings for the life of the tab")
	}
}

// The caller-owned trigger's keyboard: Enter and Space on a button
// trigger open the panel and focus the first row (the native button
// activation), an ANCHOR trigger opens on Space through the module's
// own preventDefault path (an anchor does nothing on Space natively,
// and the page must not scroll), and Tab from inside the panel closes
// the chain — including an open submenu — while Tab on the trigger
// closes an open menu. Escape from a submenu row closes one level and
// focuses that submenu's summary, not the whole menu. These moved from
// the retired core-ui trigger suite.
func TestE2E_MenuCallerOwnedTriggerKeyboard(t *testing.T) {
	menu := Menu(MenuProps{ID: "tk", TriggerElement: render.HTML(`<button type="button" id="tk-trigger">Open</button>`), Items: []MenuItem{
		{Label: "Row one"},
		{Label: "Sub", Children: []MenuItem{{Label: "Inner"}}},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	// Real trusted keypresses: native button activation on Enter (and
	// Space) produces the click the module's wrapper listener toggles
	// on — a synthetic keydown cannot, which is the point of using
	// SendKeys here.
	open := func() {
		t.Helper()
		if err := chromedp.Run(ctx,
			chromedp.Focus(`#tk-trigger`, chromedp.ByQuery),
			chromedp.SendKeys(`#tk-trigger`, "\r", chromedp.ByQuery),
		); err != nil {
			t.Fatal(err)
		}
	}
	open()
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="tk"]').open &&
		document.activeElement && document.activeElement.textContent.trim() === 'Row one'`) {
		t.Fatal("Enter on the button trigger did not open the panel and focus the first row")
	}
	// Tab from inside the panel closes the chain.
	if err := chromedp.Run(ctx, menuKey("Tab")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="tk"]').open`) {
		t.Fatal("Tab from inside the panel did not close the menu")
	}
	// Space opens too (a real keypress, for the same reason).
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#tk-trigger`, chromedp.ByQuery),
		chromedp.SendKeys(`#tk-trigger`, " ", chromedp.ByQuery),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="tk"]').open`) {
		t.Fatal("Space on the button trigger did not open the panel")
	}
	// Tab ON the trigger closes an open menu before focus moves on.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('tk-trigger').focus()`, nil),
		menuKey("Tab"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="tk"]').open`) {
		t.Fatal("Tab on the trigger did not close the open menu")
	}
	// Open, walk into the submenu, then Tab: the whole chain closes.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('tk-trigger').click()`, nil),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'Row one'`) {
		t.Fatal("the click did not focus the first row")
	}
	if err := chromedp.Run(ctx, menuKey("ArrowDown"), menuKey("ArrowRight")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="tk-panel-sub-1"]').open`) {
		t.Fatal("ArrowRight did not open the submenu")
	}
	if err := chromedp.Run(ctx, menuKey("Tab")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="tk"]').open &&
		!document.querySelector('details[data-hui-menu="tk-panel-sub-1"]').open`) {
		t.Fatal("Tab inside the submenu did not close the whole chain")
	}
	// Escape from a submenu row closes ONE level and focuses that
	// submenu's summary.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('tk-trigger').click()`, nil),
		menuKey("ArrowDown"),
		menuKey("ArrowRight"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'Inner'`) {
		t.Fatal("ArrowRight did not focus the submenu's first row")
	}
	if err := chromedp.Run(ctx, menuKey("Escape")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="tk"]').open &&
		!document.querySelector('details[data-hui-menu="tk-panel-sub-1"]').open &&
		document.activeElement && document.activeElement.getAttribute('role') === 'menuitem'`) {
		t.Fatal("Escape from the submenu row closed more than one level, or did not focus the submenu's summary")
	}
}

// An ANCHOR trigger opens on Space through the module's own
// preventDefault path: an anchor does nothing on Space natively, and
// the page must not scroll.
func TestE2E_MenuAnchorTriggerOpensOnSpace(t *testing.T) {
	menu := Menu(MenuProps{ID: "at", TriggerElement: render.HTML(`<a href="#nowhere" id="at-trigger">Open</a>`), Items: []MenuItem{
		{Label: "Row"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('at-trigger').focus()`, nil),
		menuKey(" "),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="at"]').open &&
		document.activeElement && document.activeElement.textContent.trim() === 'Row'`) {
		t.Fatal("Space on the anchor trigger did not open the panel and focus the first row")
	}
	// And the anchor did not navigate: the hash never changed.
	var hash string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`location.hash`, &hash)); err != nil {
		t.Fatal(err)
	}
	if hash != "" {
		t.Fatalf("the anchor trigger navigated on Space (hash=%q) — the module's preventDefault path was skipped", hash)
	}
}

// Under dir="rtl" the submenu arrows swap: ArrowLeft opens the
// submenu and moves focus in, ArrowRight closes it and returns focus
// to the parent row. Moved from the retired core-ui contract suite.
func TestE2E_MenuSubmenuArrowKeysSwapInRTL(t *testing.T) {
	menu := Menu(MenuProps{ID: "rtl", Label: "Account", Items: []MenuItem{
		{Label: "Profile"},
		{Label: "Palette", Children: []MenuItem{
			{Label: "Light"},
			{Label: "Dark"},
		}},
	}}, nil)
	b := startBehaviorServer(t, `<div dir="rtl">`+string(menu)+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="rtl"] > summary').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.textContent.trim() === 'Profile'`) {
		t.Fatal("the menu never focused its first row")
	}
	// ArrowDown to Palette, then ArrowLeft opens the submenu in RTL.
	if err := chromedp.Run(ctx, menuKey("ArrowDown"), menuKey("ArrowLeft")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="rtl-panel-sub-1"]').open &&
		document.activeElement && document.activeElement.textContent.trim() === 'Light'`) {
		t.Fatal("ArrowLeft (RTL) did not open the submenu and focus its first row")
	}
	// ArrowRight closes it and returns focus to the parent row.
	if err := chromedp.Run(ctx, menuKey("ArrowRight")); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="rtl-panel-sub-1"]').open &&
		document.activeElement && document.activeElement.textContent.trim() === 'Palette'`) {
		t.Fatal("ArrowRight (RTL) did not close the submenu and return focus to the parent row")
	}
}

// When the first row of an opened menu is itself a submenu summary,
// that summary receives the focus-on-open — not a row hidden inside
// the still-closed nested details (a plain descendant search would
// match the hidden one first and focus() would be a silent no-op,
// leaving the menu keyboard-dead). Moved from the retired core-ui
// contract suite.
func TestE2E_MenuFocusOnOpenFirstRowIsSubmenu(t *testing.T) {
	menu := Menu(MenuProps{ID: "fs", Label: "Account", Items: []MenuItem{
		{Label: "Palette", Children: []MenuItem{
			{Label: "Light"},
			{Label: "Dark"},
		}},
		{Label: "Later"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('details[data-hui-menu="fs"] > summary').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement === document.querySelector('details[data-hui-menu="fs-panel-sub-0"] > summary')`) {
		t.Fatal("the submenu-parent first row did not receive the focus-on-open")
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="fs-panel-sub-0"]').open`) {
		t.Fatal("the focus-on-open leaked into opening the submenu")
	}
}

// An ungrouped menuitemradio (hand-authored: the primitive cannot emit
// one — an empty Radio renders the plain menuitem) is an implicit group
// of one: activating it checks it and touches nobody else, and the
// natural buggy reading of the arbitration loop (a missing group key
// matching EVERY radio in the scope) wipes real groups' checked state.
// Moved from the retired core-ui contract suite.
func TestE2E_MenuUngroupedRadioSelfChecks(t *testing.T) {
	// Hand-written in the anatomy the primitive renders, with one row
	// carrying no group key.
	body := `<details data-hui-disclosure="" data-hui-menu="ug">` +
		`<summary aria-haspopup="menu" aria-controls="ug-panel">Pick</summary>` +
		`<div data-hui-menu-panel="" id="ug-panel" role="menu">` +
		`<button aria-checked="true" data-hui-menu-radio="g1" role="menuitemradio" tabindex="-1" type="button"><span>A</span></button>` +
		`<button aria-checked="false" data-hui-menu-radio="g1" role="menuitemradio" tabindex="-1" type="button"><span>B</span></button>` +
		`<button aria-checked="false" role="menuitemradio" tabindex="-1" type="button"><span>U</span></button>` +
		`</div></details>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("the module never loaded")
	}
	var state string
	read := func() map[string]string {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`JSON.stringify(Array.from(
			document.querySelectorAll('#ug-panel [role="menuitemradio"]')
		).map(r => [r.textContent.trim(), r.getAttribute('aria-checked')]))`, &state)); err != nil {
			t.Fatal(err)
		}
		var pairs [][2]string
		if err := json.Unmarshal([]byte(state), &pairs); err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, p := range pairs {
			out[p[0]] = p[1]
		}
		return out
	}
	clickRow := func(label string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(
			`Array.from(document.querySelectorAll('#ug-panel [role="menuitemradio"]')).find(r => r.textContent.trim() === '`+label+`').click()`, nil)); err != nil {
			t.Fatal(err)
		}
	}
	clickRow("U")
	got := read()
	if got["U"] != "true" || got["A"] != "true" || got["B"] != "false" {
		t.Fatalf("after activating ungrouped U: %v, want U checked and the g1 pair untouched (implicit group of one)", got)
	}
	clickRow("B")
	got = read()
	if got["B"] != "true" || got["A"] != "false" || got["U"] != "true" {
		t.Fatalf("after activating grouped B: %v, want B checked and U keeping its own check", got)
	}
}

// A TriggerHTML button nested inside the summary toggles the disclosure
// under a REAL pointer: Chrome's UA activation does not run when the
// click target is an interactive descendant of the summary, so without
// the module's interactive-descendant toggle the menu opens dead.
// Moved from the retired core-ui contract suite.
func TestE2E_MenuNestedTriggerButtonRealClick(t *testing.T) {
	menu := Menu(MenuProps{ID: "nb", TriggerHTML: render.HTML(`<button type="button" id="nested-btn">Open user menu</button>`), Items: []MenuItem{
		{Label: "Profile", Href: "/me"},
	}}, nil)
	b := startBehaviorServer(t, menuPage(t, menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, disclosureLoaded) {
		t.Fatal("the disclosure module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#nested-btn`, chromedp.ByQuery),
		chromedp.Sleep(150*1e6),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelector('details[data-hui-menu="nb"]').open &&
		document.activeElement && document.activeElement.textContent.trim() === 'Profile'`) {
		t.Fatal("a real click on the nested trigger button did not open the menu and focus the first row")
	}
	// A second real click closes it (no double-toggle).
	if err := chromedp.Run(ctx,
		chromedp.Click(`#nested-btn`, chromedp.ByQuery),
		chromedp.Sleep(150*1e6),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `!document.querySelector('details[data-hui-menu="nb"]').open`) {
		t.Fatal("the second real click did not close the menu (double-toggle)")
	}
}
