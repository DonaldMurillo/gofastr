package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestTagInputEnterDuringCompositionDoesNotCommit pins the IME contract
// for the taginput module: the Enter that CONFIRMS a composition candidate
// (Japanese kana→kanji, Chinese pinyin, Korean) must not commit a tag.
// During composition, keydown carries isComposing=true and the field holds
// the pre-conversion text; committing there both ships the raw romaji /
// pinyin as a tag and clears the composition.
//
// The repo already holds this contract for the other Enter handlers:
// src/widgethelpers.js (submit-on-enter) and src/shortcut.js both return
// on e.isComposing. taginput.js's keydown handler is the one Enter path
// that never got the guard.
//
// The synthetic event sets isComposing in the KeyboardEventInit dict,
// which Chrome honours, and a compositionstart is dispatched first so the
// field is observably mid-composition.
func TestTagInputEnterDuringCompositionDoesNotCommit(t *testing.T) {
	g := startGadgetServer(t, `[]`, tagInputPage)
	ctx := newSeedBrowserCtx(t)

	var afterCompose, afterPlain, composingEcho string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#tags`, chromedp.ByID),
		chromedp.Sleep(600*time.Millisecond),

		// Echo back what the browser did with the init dict, so a
		// browser that drops isComposing fails the fixture check
		// instead of vacuously passing the commit assertion.
		chromedp.Evaluate(`(function(){
			var seen = null;
			document.addEventListener('keydown', function (e) { seen = e.isComposing; }, { capture: true, once: true });
			document.getElementById('tags').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true, cancelable: true }));
			return String(seen);
		})()`, &composingEcho),

		// Mid-composition Enter: field holds the pre-conversion text,
		// composition is open. No tag may be committed.
		chromedp.Evaluate(`(function(){
			var el = document.getElementById('tags');
			el.focus();
			el.value = 'nihongo';
			el.dispatchEvent(new CompositionEvent('compositionstart', { bubbles: true }));
			el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: true, bubbles: true, cancelable: true }));
		})()`, nil),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &afterCompose),

		// Control: a plain Enter (no composition) must still commit —
		// otherwise the fix over-reached and broke the primary contract.
		chromedp.Evaluate(`(function(){
			var el = document.getElementById('tags');
			el.focus();
			el.value = 'committed';
			el.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', isComposing: false, bubbles: true, cancelable: true }));
		})()`, nil),
		chromedp.Sleep(120*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelectorAll('.ui-tag-input__chip').length)`, &afterPlain),
	); err != nil {
		t.Fatal(err)
	}

	if composingEcho != "true" {
		t.Fatalf("fixture: synthetic keydown did not carry isComposing=true (echo = %s); the commit assertion below would be vacuous", composingEcho)
	}
	// Page ships one SSR chip ("go"); the composing Enter must leave that
	// count unchanged AND leave the text in the field.
	if afterCompose != "1" {
		t.Errorf("IME: Enter during composition committed a tag — chip count went 1 -> %s; the pre-conversion text must stay in the field until composition ends", afterCompose)
	}
	if afterPlain != "2" {
		t.Errorf("control: plain Enter must still commit a tag — chip count = %s, want 2 (SSR 'go' + 'committed')", afterPlain)
	}
}
