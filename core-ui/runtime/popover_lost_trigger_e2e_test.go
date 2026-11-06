package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestPopoverClosesWithoutTrigger(t *testing.T) {
	for _, phase := range []string{"anchor", "scroll"} {
		for _, change := range []string{"remove", "hide"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				url := ctxSwitchServer(t, "")
				ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
				if err := chromedp.Run(ctx,
					chromedp.Navigate(url),
					chromedp.WaitVisible(`#ready`, chromedp.ByID),
					chromedp.Click(`#a`, chromedp.ByID),
					chromedp.Poll(`!!window.__gofastr._widgets?.row`, nil),
					chromedp.Evaluate(`window.__trigger = document.getElementById('a'); window.__gofastr.loadModule('popover')`, nil, chromedp.EvalAsValue),
					chromedp.Poll(`typeof window.__gofastr._anchorPopover === 'function'`, nil),
				); err != nil {
					t.Fatal(err)
				}
				anchor := `window.__gofastr._anchorPopover('row', window.__trigger, 'bottom')`
				if phase == "scroll" {
					if err := chromedp.Run(ctx,
						chromedp.Evaluate(anchor, nil, chromedp.EvalAsValue),
						chromedp.Poll(`!!window.__gofastr._widgets.row.anchorTrigger`, nil),
					); err != nil {
						t.Fatal(err)
					}
				}
				mutation := `window.__trigger.remove();`
				if change == "hide" {
					mutation = `window.__trigger.hidden = true;`
				}
				if phase == "anchor" {
					mutation += anchor
				} else {
					mutation += `window.dispatchEvent(new Event('scroll'));`
				}
				var state string
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(mutation, nil, chromedp.EvalAsValue),
					chromedp.Sleep(100*time.Millisecond),
					chromedp.Evaluate(`JSON.stringify({open: !!window.__gofastr._widgets.row, active: window.__trigger.classList.contains('is-popover-trigger-active'), mark: window.__trigger.hasAttribute('data-cui-popover-trigger')})`, &state),
				); err != nil {
					t.Fatal(err)
				}
				want := `{"open":false,"active":false,"mark":false}`
				if state != want {
					t.Fatalf("lost trigger: %s, want %s", state, want)
				}
			})
		}
	}
}

func TestPopoverIgnoresStalePlacement(t *testing.T) {
	for _, action := range []string{"reopen", "reanchor"} {
		t.Run(action, func(t *testing.T) {
			url := ctxSwitchServer(t, "")
			ctx := chromedptest.Context(t, chromedptest.Timeout(30*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(url),
				chromedp.WaitVisible(`#ready`, chromedp.ByID),
				chromedp.Click(`#a`, chromedp.ByID),
				chromedp.Poll(`!!window.__gofastr._widgets?.row`, nil),
				chromedp.Evaluate(`window.__gofastr.loadModule('popover')`, nil, chromedp.EvalAsValue),
				chromedp.Poll(`typeof window.__gofastr._anchorPopover === 'function'`, nil),
				chromedp.Evaluate(`window.__oldTrigger = document.getElementById('a'); window.__gofastr._anchorPopover('row', window.__oldTrigger, 'bottom')`, nil, chromedp.EvalAsValue),
				chromedp.Poll(`!!window.__gofastr._widgets.row.anchorTrigger`, nil),
				chromedp.Evaluate(`window.__raf = window.requestAnimationFrame; window.requestAnimationFrame = callback => { window.__place = callback; return 1 }; window.__oldTrigger.remove(); window.dispatchEvent(new Event('scroll')); window.requestAnimationFrame = window.__raf`, nil),
			); err != nil {
				t.Fatal(err)
			}
			if action == "reopen" {
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`window.__gofastr.closeWidget('row')`, nil),
					chromedp.Click(`#b`, chromedp.ByID),
					chromedp.Poll(`!!window.__gofastr._widgets?.row`, nil),
				); err != nil {
					t.Fatal(err)
				}
			}
			var open bool
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.__gofastr._anchorPopover('row', document.getElementById('b'), 'bottom')`, nil, chromedp.EvalAsValue),
				chromedp.Poll(`window.__gofastr._widgets?.row?.anchorTrigger?.id === 'b'`, nil),
				chromedp.Evaluate(`window.__place(); !!window.__gofastr._widgets.row`, &open),
			); err != nil {
				t.Fatal(err)
			}
			if !open {
				t.Fatal("stale placement closed the current popover")
			}
		})
	}
}
