package hosttest

import (
	"context"
	"testing"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestCancelledEnterKeepsCombobox(t *testing.T) {
	for _, rows := range []string{"ssr", "island", "search"} {
		for _, listener := range []string{"document-before-boot", "input"} {
			t.Run(rows+"/"+listener, func(t *testing.T) {
				server := comboboxLinkSite(t, rows, "", "")
				const cancel = `event => {
					if (event.key === 'Enter' && event.target.id === 'links' && window.cancelComboboxEnter !== false) {
						window.cancelledEnterTrusted = event.isTrusted; event.preventDefault();
					}
				}`
				var beforeBoot []string
				if listener == "document-before-boot" {
					beforeBoot = []string{`document.addEventListener('keydown', ` + cancel + `);`}
				}
				ctx := comboboxLinkPage(t, server, rows, "", beforeBoot...)
				recordComboboxHistory(t, ctx)
				enter := chromedp.ActionFunc(func(ctx context.Context) error {
					if err := input.DispatchKeyEvent(input.KeyRawDown).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13).Do(ctx); err != nil {
						return err
					}
					return input.DispatchKeyEvent(input.KeyUp).WithKey("Enter").WithCode("Enter").WithWindowsVirtualKeyCode(13).Do(ctx)
				})
				if listener == "input" {
					if err := chromedp.Run(ctx, chromedp.Evaluate(`pickedInput.addEventListener('keydown', `+cancel+`);`, nil)); err != nil {
						t.Fatal(err)
					}
				}
				if err := chromedp.Run(ctx,
					chromedp.KeyEvent(kb.Home),
					chromedp.Evaluate(`window.enterInputValue = pickedInput.value; window.enterActiveOption = pickedInput.getAttribute('aria-activedescendant')`, nil),
					chromedp.Poll(`!!enterActiveOption && !!document.getElementById(enterActiveOption).getAttribute('href')`, nil),
					enter,
				); err != nil {
					t.Fatal(err)
				}
				var state struct {
					Path                                        string
					Pushes, Replaces, Navigations, Programmatic int
					Trusted, Unchanged                          bool
				}
				if err := chromedp.Run(ctx, chromedp.Evaluate(`({
					Path: location.pathname + location.search + location.hash,
					Pushes: historyPushes, Replaces: historyReplaces, Navigations: navEvents,
					Programmatic: programmaticPicks, Trusted: cancelledEnterTrusted,
					Unchanged: pickedInput.value === enterInputValue && !pickedList.hidden &&
						pickedInput.getAttribute('aria-activedescendant') === enterActiveOption
				})`, &state)); err != nil {
					t.Fatal(err)
				}
				if state.Path != "/" || state.Pushes != 0 || state.Replaces != 0 || state.Navigations != 0 || state.Programmatic != 0 || !state.Trusted || !state.Unchanged {
					t.Fatalf("cancelled Enter changed the combobox or navigated: %+v", state)
				}
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`window.cancelComboboxEnter = false`, nil),
					enter,
					chromedp.Poll(`programmaticPicks === 1 && historyPushes === 1 && navEvents === 1`, nil),
				); err != nil {
					t.Fatalf("uncancelled Enter did not navigate once: %v", err)
				}
			})
		}
	}
}
