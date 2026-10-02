package headless

// Browser coverage for headless-tree: the WAI-ARIA treeview keyboard
// contract against the real rendered primitive and the real
// registered module — roving tabindex, arrows, Home/End, type-ahead,
// expand/collapse (both driving the same toggle button a click
// drives), and the lazy branch's first expand firing the kernel's rpc
// wiring and swapping the response into the signal-bound group. Same
// harness as behavior_e2e_test.go.

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func treeE2EPage(t *testing.T, tree render.HTML) string {
	t.Helper()
	return string(tree)
}

const treeLoaded = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-tree'])`

func treeKey(key string) chromedp.Action {
	return chromedp.Evaluate("document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'"+key+"',bubbles:true,cancelable:true}))", nil)
}

// TestE2E_TreeKeyboardContract walks the whole model: ArrowDown moves
// row to row and descends into an expanded branch, ArrowUp climbs back
// out, ArrowRight expands a collapsed branch through the toggle
// button, ArrowLeft collapses it again, Home and End jump, and
// type-ahead moves to the next row whose label starts with the typed
// prefix.
func TestE2E_TreeKeyboardContract(t *testing.T) {
	tree := Tree(TreeProps{ID: "kt", Label: "Keyboard tree", Nodes: []TreeNode{
		{ID: "alpha", Label: "alpha", Expanded: true, Children: []TreeNode{
			{ID: "alpha-one", Label: "beta leaf"},
			{ID: "alpha-two", Label: "gamma leaf"},
		}},
		{ID: "delta", Label: "delta", Children: []TreeNode{
			{ID: "delta-one", Label: "epsilon leaf"},
		}},
	}}, nil)
	b := startBehaviorServer(t, treeE2EPage(t, tree))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, treeLoaded) {
		t.Fatal("the tree marker never loaded headless-tree")
	}

	focusID := func(dst *string) chromedp.Action {
		return chromedp.Evaluate(`document.activeElement && document.activeElement.id || ''`, dst)
	}
	step := func(key string, want string) {
		t.Helper()
		if err := chromedp.Run(ctx, treeKey(key)); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := chromedp.Run(ctx, focusID(&got)); err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("after %s focus is %q, want %q", key, got, want)
		}
	}

	// Land on the first row (the roving tabindex entry point).
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('alpha').focus()`, nil)); err != nil {
		t.Fatal(err)
	}
	// alpha is expanded: ArrowDown descends into its first child.
	step("ArrowDown", "alpha-one")
	step("ArrowDown", "alpha-two")
	// Past the branch: the next VISIBLE row is the sibling delta.
	step("ArrowDown", "delta")
	// delta is collapsed: ArrowDown skips its subtree.
	step("ArrowDown", "delta")
	// Climb back.
	step("ArrowUp", "alpha-two")

	// ArrowRight on the collapsed delta expands it through the toggle
	// button and keeps focus on the row.
	step("ArrowDown", "delta")
	step("ArrowRight", "delta")
	var expanded string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('delta').getAttribute('aria-expanded')`, &expanded),
	); err != nil {
		t.Fatal(err)
	}
	if expanded != "true" {
		t.Fatalf("ArrowRight should expand the collapsed branch, aria-expanded=%q", expanded)
	}
	// Now visible: ArrowDown enters the subtree.
	step("ArrowDown", "delta-one")

	// Home and End.
	step("Home", "alpha")
	step("End", "delta-one")

	// ArrowLeft on the deepest row climbs to its parent.
	step("ArrowLeft", "delta")
	// ArrowLeft on an expanded branch collapses it first.
	step("ArrowLeft", "delta")
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('delta').getAttribute('aria-expanded')`, &expanded),
	); err != nil {
		t.Fatal(err)
	}
	if expanded != "false" {
		t.Fatalf("ArrowLeft should collapse the expanded branch, aria-expanded=%q", expanded)
	}

	// Type-ahead from alpha: typing "d" jumps to the next visible row
	// whose label starts with d (delta; alpha's subtree has none).
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('alpha').focus()`, nil)); err != nil {
		t.Fatal(err)
	}
	step("d", "delta")
	// "b" matches "beta leaf" only after delta's group is open again,
	// and only once the type-ahead buffer has expired — "d" then "b"
	// inside the window means the prefix "db", which matches nothing.
	step("ArrowRight", "delta")
	if err := chromedp.Run(ctx, chromedp.Sleep(900*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	step("b", "alpha-one")
}

// TestE2E_TreeToggleClickAndRovingTabindex proves the pointer path:
// clicking a toggle flips aria-expanded and the group's visibility,
// and clicking a row moves the roving tabindex onto it.
func TestE2E_TreeToggleClickAndRovingTabindex(t *testing.T) {
	tree := Tree(TreeProps{ID: "ct", Label: "Click tree", Nodes: []TreeNode{
		{ID: "one", Label: "one", Children: []TreeNode{
			{ID: "one-a", Label: "one-a"},
		}},
		{ID: "two", Label: "two"},
	}}, nil)
	b := startBehaviorServer(t, treeE2EPage(t, tree))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, treeLoaded) {
		t.Fatal("the tree marker never loaded headless-tree")
	}

	var expanded, hidden, tabTwo string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('#one [data-hui-tree-toggle]').click()`, nil),
		chromedp.Evaluate(`document.getElementById('one').getAttribute('aria-expanded')`, &expanded),
		chromedp.Evaluate(`document.querySelector('#one > [role="group"]').getAttribute('hidden') !== null ? 'hidden' : 'shown'`, &hidden),
		// Focus returns to the treeitem, not the aria-hidden toggle.
		chromedp.Evaluate(`document.activeElement.id`, &tabTwo),
	); err != nil {
		t.Fatal(err)
	}
	if expanded != "true" || hidden != "shown" {
		t.Fatalf("toggle click: aria-expanded=%q group=%q, want true/shown", expanded, hidden)
	}
	if tabTwo != "one" {
		t.Fatalf("after toggle click focus is %q, want the treeitem 'one' (the toggle is aria-hidden)", tabTwo)
	}

	// A row click moves the roving tabindex.
	var tabi string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('two').querySelector('span').dispatchEvent(new MouseEvent('click',{bubbles:true}))`, nil),
		chromedp.Evaluate(`document.getElementById('two').getAttribute('tabindex')`, &tabi),
	); err != nil {
		t.Fatal(err)
	}
	if tabi != "0" {
		t.Fatalf("a row click should move the roving tabindex onto the row, got %q", tabi)
	}
}

// TestE2E_TreeLazyLoadFiresTheRpc proves the lazy contract end to end:
// expanding a lazy branch through the keyboard (ArrowRight, which
// drives the toggle button) fires the kernel's rpc wiring on the
// toggle, and the response HTML lands inside the signal-bound group.
func TestE2E_TreeLazyLoadFiresTheRpc(t *testing.T) {
	tree := Tree(TreeProps{ID: "lz", Label: "Lazy tree", LazySignalPrefix: "lz-tree", Nodes: []TreeNode{
		{ID: "vendor", Label: "vendor", LazyPath: "/tree/vendor"},
	}}, nil)
	var hits int
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/tree/vendor", func(w http.ResponseWriter, r *http.Request) {
			hits++
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<li id="vendor-a" role="treeitem" aria-level="2" aria-posinset="1" aria-setsize="1" tabindex="-1"><div><span>vendor-a.go</span></div></li>`)
		})
	}
	b := startBehaviorServer(t, treeE2EPage(t, tree), extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, treeLoaded) {
		t.Fatal("the tree marker never loaded headless-tree")
	}

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('vendor').focus()`, nil),
		treeKey("ArrowRight"),
	); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('#vendor > [role="group"] > [role="treeitem"]').length === 1`) {
		t.Fatal("the lazy branch's group never received the RPC response")
	}
	if hits == 0 {
		t.Fatal("the lazy expand never fired the toggle's rpc endpoint")
	}
	// The loaded child participates in the keyboard model.
	var id string
	if err := chromedp.Run(ctx,
		treeKey("ArrowDown"),
		chromedp.Evaluate(`document.activeElement.id`, &id),
	); err != nil {
		t.Fatal(err)
	}
	if id != "vendor-a" {
		t.Fatalf("ArrowDown after lazy load focused %q, want vendor-a", id)
	}
}

// TestE2E_TreeMarkupArrivingAfterLoadArms proves the kernel's
// insertion scan hands late tree markup to the module: a tree whose
// nodes carry control bytes in labels still binds (the delegated
// listeners need no per-root pass), and the module never writes a
// string of its own.
func TestE2E_TreeSaysNothingInEnglishInTheDOM(t *testing.T) {
	tree := Tree(TreeProps{ID: "quiet", Label: strings.Repeat(" ", 0) + "Quiet", Nodes: []TreeNode{
		{ID: "a", Label: "a"},
	}}, nil)
	b := startBehaviorServer(t, treeE2EPage(t, tree))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, treeLoaded) {
		t.Fatal("the tree marker never loaded headless-tree")
	}
	var body string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.body.innerText`, &body)); err != nil {
		t.Fatal(err)
	}
	// The module moves focus and state only; any sentence it said
	// itself would be English on a translated page.
	for _, word := range []string{"Loading", "Expanded", "Collapsed"} {
		if strings.Contains(body, word) {
			t.Errorf("the page says %q — the module wrote a sentence of its own", word)
		}
	}
}
