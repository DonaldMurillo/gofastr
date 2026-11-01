package ui

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A drawer the server rendered open on a phone-width first load takes
// focus, and Tab with focus outside it (the page body, background nav)
// goes into the drawer instead of the page behind the scrim.
func TestSSROpenDrawerTakesFocus(t *testing.T) {
	body := PaneHost(PaneHostConfig{
		Primary: render.Join(
			render.Tag("a", map[string]string{"id": "bg-nav", "href": "#nav"}, render.Text("Background nav")),
			render.Text("The list"),
		),
		Secondary: render.Join(
			render.Tag("a", map[string]string{"id": "p-first", "href": "#a"}, render.Text("first")),
			render.Tag("a", map[string]string{"id": "p-last", "href": "#b"}, render.Text("last")),
		),
		SecondaryOpen: true,
	})
	srv := paneHostTestPage(t, string(body))
	ctx := chromedptest.Context(t)
	var onLoad, afterTab, afterShiftTab string
	tab := func(shift string) chromedp.Action {
		return chromedp.Evaluate(`document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', shiftKey: `+shift+`, bubbles: true, cancelable: true}))`, nil)
	}
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(390, 844),
		chromedp.Navigate(srv.URL),
		chromedp.Poll(paneHostDrawerLoaded, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.activeElement.id`, &onLoad),
		// Focus behind the drawer: Tab enters the drawer at its first
		// tabbable, Shift+Tab at its last.
		chromedp.Evaluate(`document.getElementById('bg-nav').focus()`, nil),
		tab("false"),
		chromedp.Evaluate(`document.activeElement.id`, &afterTab),
		chromedp.Evaluate(`document.activeElement.blur()`, nil),
		tab("true"),
		chromedp.Evaluate(`document.activeElement.id`, &afterShiftTab),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if onLoad != "p-first" {
		t.Errorf("first load focus on %q, want the open drawer's first tabbable p-first", onLoad)
	}
	if afterTab != "p-first" {
		t.Errorf("Tab from background nav went to %q, want p-first", afterTab)
	}
	if afterShiftTab != "p-last" {
		t.Errorf("Shift+Tab from the body went to %q, want p-last", afterShiftTab)
	}
}
