package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Safari ends a composition with a keydown whose isComposing is already
// false and whose keyCode is the IME's 229. That Enter confirms the
// conversion; it must not commit the field as a tag (found by
// core-ui/check's keyguard lint, the combobox's e2988bd2 fix applied to
// the tag field).
func TestE2E_TagInputSafariCompositionEnterDoesNotCommit(t *testing.T) {
	body := TagInput(TagInputProps{Name: "tags", Label: "Tags", Values: []string{"go"}}, nil)
	b := startBehaviorServer(t, string(body))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CollectionsBehaviorName)) {
		t.Fatal("the tag field marker never loaded headless-collections")
	}
	var got string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(function(){
		var f = document.querySelector('[data-hui-tag-input-field]');
		var chips = function () { return document.querySelectorAll('[data-hui-tag-input-remove]').length; };
		f.value = '日本語';
		f.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', keyCode:229, bubbles:true, cancelable:true}));
		var safari = chips() + ':' + f.value;
		f.dispatchEvent(new KeyboardEvent('keydown', {key:'Enter', bubbles:true, cancelable:true}));
		return safari + '|' + chips();
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	if got != "1:日本語|2" {
		t.Fatalf("[chips:field after Safari's composition Enter|chips after a plain Enter] = %q, want 1:日本語|2", got)
	}
}
