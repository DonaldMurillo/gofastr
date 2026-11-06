package headless

import (
	"fmt"
	"testing"

	"github.com/chromedp/chromedp"
)

func TestComboboxUpStartsAtLast(t *testing.T) {
	for _, count := range []int{1, 2, 4} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			options := make([]ComboboxOption, count)
			for index := range options {
				options[index].Label = fmt.Sprintf("Choice %d", index)
			}
			server := startBehaviorServer(t, string(Combobox(ComboboxProps{
				ID: "up", Name: "q", Label: "Search", Options: options,
			}, nil)))
			ctx := behaviorPage(t, server)
			if !pollTrue(ctx, comboboxLoaded) {
				t.Fatal("combobox module did not load")
			}
			var active string
			if err := chromedp.Run(ctx,
				chromedp.Click(`#up`, chromedp.ByID),
				chromedp.Evaluate(`document.getElementById('up').dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowUp', bubbles: true, cancelable: true}))`, nil),
				chromedp.Evaluate(`document.getElementById('up').getAttribute('aria-activedescendant')`, &active),
			); err != nil {
				t.Fatal(err)
			}
			if want := fmt.Sprintf("up-listbox-opt-%d", count-1); active != want {
				t.Fatalf("ArrowUp selected %q, want %q", active, want)
			}
		})
	}
}
