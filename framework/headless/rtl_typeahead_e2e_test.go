package headless

// Keyboard models that follow the screen and the reader's typing: a
// row of radios flips in RTL, and a repeated letter cycles.

import (
	"testing"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// The theme control is a horizontal radio group: in RTL, ArrowLeft goes
// to the next option, as the tabs and the carousel already do; Down
// keeps document order.
func TestE2E_ThemeRadiosFlipInRTL(t *testing.T) {
	body := `<div dir="rtl"><div data-hui-theme-toggle role="radiogroup" aria-label="Theme">` +
		`<button id="o-light" type="button" role="radio" data-hui-theme-option="light">L</button>` +
		`<button id="o-dark" type="button" role="radio" data-hui-theme-option="dark">D</button>` +
		`<button id="o-auto" type="button" role="radio" data-hui-theme-option="auto">A</button></div></div>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-navigation")) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Focus(`#o-light`, chromedp.ByID), chromedp.KeyEvent(kb.ArrowLeft)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.activeElement.id`); got != "o-dark" {
		t.Fatalf("ArrowLeft from the first option in RTL moved to %q, want the next option o-dark", got)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.ArrowRight)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.activeElement.id`); got != "o-light" {
		t.Fatalf("ArrowRight in RTL moved to %q, want the previous option o-light", got)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.ArrowDown)); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.activeElement.id`); got != "o-dark" {
		t.Fatalf("ArrowDown in RTL moved to %q, want the next option in document order o-dark", got)
	}
}

// Menu type-ahead: the same letter typed twice in quick succession
// moves to the NEXT item starting with it (APG menu / listbox
// type-ahead), and a third press wraps.
func TestE2E_MenuTypeaheadRepeatedLetterCycles(t *testing.T) {
	menu := Menu(MenuProps{ID: "ta", Label: "Go", Items: []MenuItem{
		{Label: "Alpha", Href: "/a"}, {Label: "Beta", Href: "/b"}, {Label: "Bravo", Href: "/c"},
	}}, nil)
	b := startBehaviorServer(t, string(menu))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, menuLoaded) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('details[data-hui-menu="ta"] > summary').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.getAttribute('role') === 'menuitem'`) {
		t.Fatal("open did not focus a row")
	}
	var label string
	if err := chromedp.Run(ctx, menuKey("b"), menuKey("b"), menuFocusLabel(&label)); err != nil {
		t.Fatal(err)
	}
	if label != "Bravo" {
		t.Fatalf("typing b, b from Alpha focused %q, want Bravo (the second b cycles)", label)
	}
	if err := chromedp.Run(ctx, menuKey("b"), menuFocusLabel(&label)); err != nil {
		t.Fatal(err)
	}
	if label != "Beta" {
		t.Fatalf("a third b focused %q, want Beta (the cycle wraps)", label)
	}
}

// The tree's type-ahead cycles the same way.
func TestE2E_TreeTypeaheadRepeatedLetterCycles(t *testing.T) {
	tree := Tree(TreeProps{ID: "tt", Label: "Files", Nodes: []TreeNode{
		{ID: "n-alpha", Label: "Alpha"}, {ID: "n-beta", Label: "Beta"}, {ID: "n-bravo", Label: "Bravo"},
	}}, nil)
	b := startBehaviorServer(t, string(tree))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr("headless-tree")) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Focus(`#n-alpha`, chromedp.ByID),
		chromedp.KeyEvent("b"),
		chromedp.KeyEvent("b"),
	); err != nil {
		t.Fatal(err)
	}
	if got := evalString(t, ctx, `document.activeElement.id`); got != "n-bravo" {
		t.Fatalf("typing b, b from Alpha focused %q, want n-bravo (the second b cycles)", got)
	}
}
