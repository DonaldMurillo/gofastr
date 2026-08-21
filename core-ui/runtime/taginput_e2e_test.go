package runtime

import (
	"strconv"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// tagInputPage is the exact SSR markup framework/ui.TagInput emits for a
// Name="tags" field seeded with one tag ("go"). It is inlined (rather than
// rendered via the Go component) because framework/ui transitively imports
// core-ui/runtime, so no test in this package can import it. Keep this in
// sync with framework/ui/taginput.go:TagInput, the runtime hydrates off
// these exact classes and data-fui-* attributes:
//   - .ui-tag-input__zone + [data-fui-tag-input-zone] wrapper
//   - one <input type=hidden class="ui-tag-input__hidden"> per seed tag
//   - the text <input> carrying id, data-fui-tag-input (form name) and
//     data-fui-tag-input-id (the field id the runtime must consume)
const tagInputPage = `
<div class="ui-tag-input" data-fui-comp="ui-tag-input">
  <label for="tags" class="ui-tag-input__label">Tags</label>
  <div class="ui-tag-input__zone" data-fui-tag-input-zone="true">
    <input type="hidden" name="tags" value="go" class="ui-tag-input__hidden">
    <input type="text" id="tags" class="ui-tag-input__field" aria-label="Tags" autocomplete="off" data-fui-tag-input="tags" data-fui-tag-input-id="tags" placeholder="Add a tag">
  </div>
</div>`

// TestTagInputRoundTrip drives a real browser against a server-rendered
// framework/ui.TagInput and proves the taginput runtime module round-trips
// chips, AND, the part that was missing, consumes
// data-fui-tag-input-id: removing a chip via its × button must return
// focus to the field identified by that id, so keyboard / screen-reader
// users stay in the input instead of falling out to <body>.
func TestTagInputRoundTrip(t *testing.T) {
	g := startGadgetServer(t, `[]`, tagInputPage)
	ctx := newSeedBrowserCtx(t)

	// typeTagValue focuses the field, sets its value, and dispatches a
	// keydown with the given key (Enter / "," / "Backspace"). Mirrors the
	// synthetic-keyboard pattern sortablelist_e2e_test.go uses; the
	// taginput handler reads .value at keydown time and branches on key.
	typeTagValue := func(value, key string) chromedp.Action {
		return chromedp.Evaluate(`(function(){
			var el = document.getElementById('tags');
			el.focus();
			el.value = `+strconv.Quote(value)+`;
			el.dispatchEvent(new KeyboardEvent('keydown', { key: `+strconv.Quote(key)+`, bubbles: true, cancelable: true }));
		})()`, nil)
	}

	var initialChips, initialHidden string
	var afterEnterChips, afterEnterHidden, afterEnterValue string
	var afterCommaChips, afterCommaHidden string
	var afterBackspaceChips, afterBackspaceHidden string
	var afterRemoveChips, activeID, activeDataID string

	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#tags`, chromedp.ByID),
		// Let the initial marker scan fetch taginput.js and let the
		// module run its hydration pass over the SSR hidden input.
		chromedp.Sleep(600*time.Millisecond),

		// Initial "go" tag was hydrated into a chip + hidden input.
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &initialChips),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-tag-input__hidden')).map(function(h){return h.value;}).join(',')`, &initialHidden),

		// Enter commits "rust" as a new chip + hidden input and clears the field.
		typeTagValue("rust", "Enter"),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &afterEnterChips),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-tag-input__hidden')).map(function(h){return h.value;}).join(',')`, &afterEnterHidden),
		chromedp.Evaluate(`document.getElementById('tags').value`, &afterEnterValue),

		// Comma commits "python" as a third chip.
		typeTagValue("python", ","),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &afterCommaChips),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-tag-input__hidden')).map(function(h){return h.value;}).join(',')`, &afterCommaHidden),

		// Backspace on an empty field removes the last chip ("python").
		typeTagValue("", "Backspace"),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &afterBackspaceChips),
		chromedp.Evaluate(`Array.from(document.querySelectorAll('.ui-tag-input__hidden')).map(function(h){return h.value;}).join(',')`, &afterBackspaceHidden),

		// Clicking the first chip's × removes it. The field is identified
		// by data-fui-tag-input-id, so focus must return there.
		chromedp.Click(`.ui-tag-input__chip-remove`, chromedp.ByQuery),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &afterRemoveChips),
		chromedp.Evaluate(`String(document.activeElement && document.activeElement.id)`, &activeID),
		chromedp.Evaluate(`String(document.activeElement && document.activeElement.getAttribute && document.activeElement.getAttribute('data-fui-tag-input-id'))`, &activeDataID),
	); err != nil {
		t.Fatal(err)
	}

	if initialChips != "1" {
		t.Errorf("hydration: expected 1 chip for the SSR 'go' tag, got %s", initialChips)
	}
	if initialHidden != "go" {
		t.Errorf("hydration: expected hidden value 'go', got %q", initialHidden)
	}
	if afterEnterChips != "2" {
		t.Errorf("Enter: expected 2 chips after committing 'rust', got %s", afterEnterChips)
	}
	if afterEnterHidden != "go,rust" {
		t.Errorf("Enter: expected hidden values 'go,rust', got %q", afterEnterHidden)
	}
	if afterEnterValue != "" {
		t.Errorf("Enter: expected the text field cleared after commit, got %q", afterEnterValue)
	}
	if afterCommaChips != "3" {
		t.Errorf("comma: expected 3 chips after committing 'python', got %s", afterCommaChips)
	}
	if afterCommaHidden != "go,rust,python" {
		t.Errorf("comma: expected hidden values 'go,rust,python', got %q", afterCommaHidden)
	}
	if afterBackspaceChips != "2" {
		t.Errorf("Backspace: expected the last chip removed (2 left), got %s", afterBackspaceChips)
	}
	if afterBackspaceHidden != "go,rust" {
		t.Errorf("Backspace: expected hidden values 'go,rust', got %q", afterBackspaceHidden)
	}
	if afterRemoveChips != "1" {
		t.Errorf("remove: expected 1 chip left after × click, got %s", afterRemoveChips)
	}
	// The load-bearing assertion for data-fui-tag-input-id: focus must
	// return to the field. Today nothing reads the id, so activeElement
	// falls through to <body> (id "" / data-fui-tag-input-id null).
	if activeID != "tags" {
		t.Errorf("remove: focus did not return to the tag field — activeElement.id = %q, want \"tags\" (data-fui-tag-input-id is not consumed)", activeID)
	}
	if activeDataID != "tags" {
		t.Errorf("remove: activeElement data-fui-tag-input-id = %q, want \"tags\"", activeDataID)
	}
}
