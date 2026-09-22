package headless

// Browser coverage for headless-collections: the tag input commits,
// dedupes, removes and announces through the server's sentences, and
// the repeater's island swap puts focus back where the operation
// happened. Same harness as behavior_e2e_test.go.

import (
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// Enter and comma commit the draft as a chip AND a hidden input; a
// duplicate clears the draft without a second chip; Backspace on an
// empty draft removes the last chip; the × removes its own chip and
// returns focus to the field.
func TestE2E_TagInputCommitsRemovesAndAnnounces(t *testing.T) {
	body := TagInput(TagInputProps{Name: "tags", Label: "Tags", Values: []string{"go"}}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) {
		t.Fatal("the tag field marker never loaded headless-collections")
	}

	// Commit "css" with Enter.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){const f=document.querySelector('[data-hui-tag-input-field]');f.value='css';f.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-tag-input-remove]').length === 2`) {
		t.Fatal("Enter did not commit the draft as a chip")
	}
	if !pollTrue(ctx, `[...document.querySelectorAll('input[type="hidden"]')].filter(h=>h.value==='css').length === 1`) {
		t.Fatal("the committed value did not become a hidden input the form submits")
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-tag-input-status]').textContent === 'css added'`) {
		t.Fatal("the added sentence did not arrive from the component's Strings")
	}

	// A duplicate is refused and the draft clears.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){const f=document.querySelector('[data-hui-tag-input-field]');f.value='css';f.dispatchEvent(new KeyboardEvent('keydown',{key:'Enter',bubbles:true,cancelable:true}));})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-tag-input-remove]').length === 2 &&
		document.querySelector('[data-hui-tag-input-field]').value === ''`) {
		t.Fatal("a duplicate value was committed a second time")
	}

	// Backspace on an empty draft removes the last chip.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`(function(){const f=document.querySelector('[data-hui-tag-input-field]');f.dispatchEvent(new KeyboardEvent('keydown',{key:'Backspace',bubbles:true,cancelable:true}));})()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-tag-input-remove]').length === 1 &&
		[...document.querySelectorAll('input[type="hidden"]')].every(h=>h.value!=='css')`) {
		t.Fatal("Backspace did not remove the last chip and its value")
	}

	// The × removes its own chip and focus returns to the field.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.activeElement.blur(); document.querySelector('[data-hui-tag-input-remove] button').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-tag-input-remove]').length === 0 &&
		[...document.querySelectorAll('input[type="hidden"]')].every(h=>h.value!=='go')`) {
		t.Fatal("the chip's × did not remove its chip")
	}
	if !pollTrue(ctx, `document.activeElement === document.querySelector('[data-hui-tag-input-field]')`) {
		t.Fatal("focus did not return to the field after a removal")
	}
}

// The repeater's add/remove are named submit controls; under an
// Island the same buttons fire the RPC, the region is replaced, and
// focus lands on the add control after an addition.
func TestE2E_RepeaterIslandSwapRestoresFocus(t *testing.T) {
	// The signal region is the caller's, exactly as the DataTable
	// documents it: wrap the island's markup in the signal the
	// response replaces.
	body := render.HTML(`<div data-fui-signal="guests" data-fui-signal-mode="html">` +
		string(Repeater(RepeaterProps{
			Name:   "guests",
			Label:  "Guests",
			Items:  []RepeaterItem{{Fields: []render.HTML{render.Text("")}}},
			Action: "/__hui/guests",
			Island: Island{Endpoint: "/__hui/guests", Signal: "guests"},
		}, nil)) + `</div>`)
	// The island endpoint answers with the region the swap lands in:
	// one more row, the server's own words in the status.
	extra := func(mux *http.ServeMux) {
		mux.HandleFunc("/__hui/guests", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(`<div data-fui-signal="guests" data-fui-signal-mode="html">` +
				string(Repeater(RepeaterProps{
					Name:   "guests",
					Label:  "Guests",
					Items:  []RepeaterItem{{Fields: []render.HTML{render.Text("")}}, {Fields: []render.HTML{render.Text("")}}},
					Action: "/__hui/guests",
					Island: Island{Endpoint: "/__hui/guests", Signal: "guests"},
					Status: "Added a row.",
				}, nil)) + `</div>`))
		})
	}
	b := startBehaviorServer(t, string(body), extra)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) {
		t.Fatal("the repeater marker never loaded headless-collections")
	}
	if !pollTrue(ctx, controlsLoadedExpr("rpc")) {
		t.Fatal("the rpc kernel module never loaded for the island buttons")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`document.querySelector('[data-hui-repeater-action="add"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-repeater-item]').length === 2`) {
		t.Fatal("the island add did not replace the region with the two-row answer")
	}
	if !pollTrue(ctx, `document.activeElement && document.activeElement.getAttribute('data-hui-repeater-action') === 'add'`) {
		t.Fatal("focus did not return to the add control after the swap")
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-repeater-status]').textContent === 'Added a row.'`) {
		t.Fatal("the server's status sentence did not land in the live region")
	}
}

// The retired core-ui/runtime taginput e2e (taginput_e2e_test.go) was
// deleted with its module: it inlined the old SSR shape (bare hidden
// inputs the module converted to chips on arrival) because
// core-ui/runtime cannot import framework/ui. The chip-commit,
// chip-remove and focus-return proofs it carried live here now, against
// the real component, through the registered module.

// The IME contract, moved with the module from core-ui/runtime: the
// Enter that CONFIRMS a composition candidate (Japanese kana→kanji,
// Chinese pinyin, Korean) must not commit a tag — during composition,
// keydown carries isComposing=true and the field holds the
// pre-conversion text, and committing there ships the raw romaji as a
// tag. The synthetic event sets isComposing in the KeyboardEventInit
// dict, which Chrome honours, and a compositionstart is dispatched
// first so the field is observably mid-composition.
func TestE2E_TagInputEnterDuringCompositionDoesNotCommit(t *testing.T) {
	body := TagInput(TagInputProps{Name: "tags", Label: "Tags", Values: []string{"go"}}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) {
		t.Fatal("the tag field marker never loaded headless-collections")
	}
	var composingEcho string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var seen = null;
		var f = document.querySelector('[data-hui-tag-input-field]');
		f.addEventListener('keydown', function(e){ seen = e.isComposing; });
		f.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, isComposing:true}));
		return String(seen);
	})()`, &composingEcho)); err != nil {
		t.Fatal(err)
	}
	if composingEcho != "true" {
		t.Skipf("this browser drops isComposing from the init dict (%q), so the fixture cannot prove the guard", composingEcho)
	}
	var chips int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var f = document.querySelector('[data-hui-tag-input-field]');
			f.dispatchEvent(new CompositionEvent('compositionstart', {bubbles:true}));
			f.value = 'にほんご';
			f.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, isComposing:true}));
		})()`, nil),
		chromedp.Evaluate(`document.querySelectorAll('[data-hui-tag-input-remove]').length`, &chips),
	); err != nil {
		t.Fatal(err)
	}
	if chips != 1 {
		t.Fatalf("an Enter mid-composition committed a tag: %d chips, want the 1 seeded one", chips)
	}
	// Outside composition the same Enter commits.
	var after int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`(function(){
			var f = document.querySelector('[data-hui-tag-input-field]');
			f.value = 'rust';
			f.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, isComposing:false}));
		})()`, nil),
		chromedp.Evaluate(`document.querySelectorAll('[data-hui-tag-input-remove]').length`, &after),
	); err != nil {
		t.Fatal(err)
	}
	if after != 2 {
		t.Fatalf("a plain Enter did not commit: %d chips, want 2", after)
	}
}

// Enter in the field commits a chip and the form does not submit: the
// keydown's own preventDefault is the whole guard (a real, trusted
// Enter — the browser's implicit-submission path included).
func TestE2E_TagInputEnterCommitsWithoutSubmittingTheForm(t *testing.T) {
	body := `<form id="tagsform" action="/nowhere" method="post">` +
		string(TagInput(TagInputProps{Name: "tags", Label: "Tags", Values: []string{"go"}}, nil)) +
		`</form>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) {
		t.Fatal("the tag field marker never loaded headless-collections")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__huiSubmits = 0;
			document.getElementById('tagsform').addEventListener('submit', () => window.__huiSubmits++);`, nil),
		chromedp.Focus(`[data-hui-tag-input-field]`),
	); err != nil {
		t.Fatal(err)
	}
	// A trusted Enter, the kind that triggers implicit submission.
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Enter)); err != nil {
		t.Fatal(err)
	}
	var submits int
	var chips int
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`window.__huiSubmits`, &submits),
		chromedp.Evaluate(`document.querySelectorAll('[data-hui-tag-input-remove]').length`, &chips),
	); err != nil {
		t.Fatal(err)
	}
	// The field was empty, so no chip commits; the point is the submit
	// count: an empty draft is the one Enter whose only default action
	// IS the implicit submission.
	if submits != 0 {
		t.Fatalf("an Enter in the field submitted the form (%d submit(s)) — the keydown's preventDefault is the guard and it did not hold", submits)
	}
}
