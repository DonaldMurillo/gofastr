package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

func TestComboboxKeepsModifiedLinks(t *testing.T) {
	server := startBehaviorServer(t, string(Combobox(ComboboxProps{
		ID: "links", Name: "q", Label: "Search", Options: []ComboboxOption{{Label: "Cedar", Href: "/cedar"}},
	}, nil)))
	ctx := behaviorPage(t, server)
	if !pollTrue(ctx, comboboxLoaded) {
		t.Fatal("combobox module did not load")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.picks = 0; window.__gofastr.navigate = () => window.picks++;`, nil)); err != nil {
		t.Fatal(err)
	}
	for _, modifiers := range []string{`metaKey: true`, `ctrlKey: true`, `shiftKey: true`, `altKey: true`, `button: 1`, `button: 2`} {
		var preserved bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const event = new MouseEvent('click', {bubbles: true, cancelable: true, `+modifiers+`});
			document.querySelector('#links-listbox [role="option"] span').dispatchEvent(event);
			return !event.defaultPrevented && window.picks === 0 && document.getElementById('links').value === '';
		})()`, &preserved)); err != nil {
			t.Fatal(err)
		}
		if !preserved {
			t.Errorf("combobox consumed a modified link (%s)", modifiers)
		}
	}
	var picked bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const event = new MouseEvent('click', {bubbles: true, cancelable: true});
		document.querySelector('#links-listbox [role="option"]').dispatchEvent(event);
		return event.defaultPrevented && window.picks === 1 && document.getElementById('links').value === 'Cedar';
	})()`, &picked)); err != nil {
		t.Fatal(err)
	}
	if !picked {
		t.Error("ordinary primary click no longer picks the option")
	}
}
