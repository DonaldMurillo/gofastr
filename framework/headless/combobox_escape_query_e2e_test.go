package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestComboboxClearStaysClosed(t *testing.T) {
	server := startBehaviorServer(t, string(Combobox(ComboboxProps{
		ID: "clear", Name: "q", Label: "Search", Options: []ComboboxOption{{Label: "Juniper"}, {Label: "Maple"}},
	}, nil)))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("combobox module did not load")
	}
	for _, query := range []string{"jun", "map"} {
		var closed bool
		if err := chromedp.Run(ctx,
			chromedp.Click(`#clear`, chromedp.ByID),
			chromedp.SendKeys(`#clear`, query, chromedp.ByID),
			chromedp.Evaluate(`(() => {
				const input = document.getElementById('clear');
				window.clears = 0;
				input.addEventListener('input', () => window.clears++, {once: true});
				for (const count of [1, 2]) input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true, cancelable: true}));
			})()`, nil),
			chromedp.Evaluate(`document.getElementById('clear').value === '' && document.getElementById('clear').getAttribute('aria-expanded') === 'false' && !document.getElementById('clear').getAttribute('aria-activedescendant') && document.getElementById('clear-listbox').hidden && window.clears === 1`, &closed),
		); err != nil {
			t.Fatal(err)
		}
		if !closed {
			t.Fatalf("clearing %q with the second Escape reopened the list", query)
		}
	}
}
