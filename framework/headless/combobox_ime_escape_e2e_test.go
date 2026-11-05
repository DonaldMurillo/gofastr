package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestComboboxIMEKeepsEscape(t *testing.T) {
	server := startBehaviorServer(t, string(Combobox(ComboboxProps{
		ID: "ime", Name: "q", Label: "Search", Options: []ComboboxOption{{Label: "Cedar"}},
	}, nil)))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("combobox module did not load")
	}
	for _, composition := range []string{`isComposing: true`, `isComposing: false, keyCode: 229`} {
		var untouched bool
		if err := chromedp.Run(ctx,
			chromedp.Click(`#ime`, chromedp.ByID),
			chromedp.Evaluate(`(() => {
				const input = document.getElementById('ime');
				const event = new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true, `+composition+`});
				input.dispatchEvent(event);
				return !event.defaultPrevented && input.getAttribute('aria-expanded') === 'true' && !document.getElementById('ime-listbox').hidden;
			})()`, &untouched),
		); err != nil {
			t.Fatal(err)
		}
		if !untouched {
			t.Errorf("Escape consumed an IME key (%s)", composition)
		}
	}
}
