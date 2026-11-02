package headless

// Enter that confirms an IME composition is the input method's, not a
// pick.

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// Chrome flags the confirming keydown isComposing; Safari sends it with
// isComposing false and the IME's keyCode 229. Neither picks the
// highlighted option over the reader's conversion.
func TestE2E_ComboboxEnterDuringCompositionDoesNotPick(t *testing.T) {
	for _, c := range []struct{ name, init string }{
		{"isComposing", `{key: 'Enter', isComposing: true, bubbles: true, cancelable: true}`},
		{"keyCode 229", `{key: 'Enter', keyCode: 229, bubbles: true, cancelable: true}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			box := Combobox(ComboboxProps{ID: "ime", Name: "q", Label: "Search", Options: []ComboboxOption{
				{Label: "Tokyo"}, {Label: "Kyoto"},
			}}, nil)
			b := startBehaviorServer(t, string(box))
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, comboboxLoaded) {
				t.Fatal("module never loaded")
			}
			if err := chromedp.Run(ctx, chromedp.Click(`#ime`, chromedp.ByID)); err != nil {
				t.Fatal(err)
			}
			got := evalString(t, ctx, `(() => {
				const i = document.getElementById('ime');
				i.value = 'to';
				i.dispatchEvent(new Event('input', {bubbles: true}));
				i.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowDown', bubbles: true, cancelable: true}));
				const picked = !!i.getAttribute('aria-activedescendant');
				i.dispatchEvent(new KeyboardEvent('keydown', `+c.init+`));
				return picked + '|' + i.value;
			})()`)
			if got != "true|to" {
				t.Fatalf("[an option highlighted|value after the composing Enter] = %q, want true|to", got)
			}
		})
	}
}
