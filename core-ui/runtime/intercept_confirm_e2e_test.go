package runtime

import (
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

func TestInterceptDefersToConfirmDialog(t *testing.T) {
	server := startInterceptStackServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.srv.URL+"/list"),
		chromedp.Click("#to-a"),
		chromedp.WaitVisible("#rec-a"),
		chromedp.Evaluate(fmt.Sprintf(`document.body.insertAdjacentHTML('beforeend', %q); window.__gofastr.loadModule('confirm').then(() => window.__gofastr.askWords({message:'Delete?'}));`, confirmTemplate), nil),
		chromedp.Poll(`!!document.querySelector('dialog:modal')`, nil),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Poll(`document.activeElement.getAttribute('data-cui-confirm-part') === 'accept'`, nil),
		chromedp.KeyEvent(kb.Escape),
		chromedp.Poll(`!document.querySelector('dialog:modal')`, nil, chromedp.WithPollingTimeout(3*time.Second)),
	); err != nil {
		t.Fatal(err)
	}
	if snapshot := readStack(ctx); len(snapshot.Layers) != 1 || snapshot.URL != "/rec/a" {
		t.Fatalf("confirm Escape closed its intercept pane: %+v", snapshot)
	}
	if err := chromedp.Run(ctx, chromedp.KeyEvent(kb.Escape), chromedp.Poll(`!document.getElementById('rec-a')`, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestInterceptGuardsUnsupportedModal(t *testing.T) {
	for _, key := range []string{"Escape", "Tab", "ShiftTab"} {
		t.Run(key, func(t *testing.T) {
			server := startInterceptStackServer(t)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(server.srv.URL+"/list"),
				chromedp.Click("#to-a"), chromedp.WaitVisible("#rec-a"),
				chromedp.Evaluate(`(()=>{
const query=document.querySelector.bind(document);
document.querySelector=selector=>{if(selector.includes(':modal')) throw new DOMException('Unsupported :modal selector','SyntaxError');return query(selector)};
const pane=document.getElementById('rec-a').parentElement;
window.paneItems=[...pane.querySelectorAll(__gofastr._focusSel)].filter(item=>item.offsetParent!==null);
window.paneItems[window.paneItems.length-1].focus();
})()`, nil)); err != nil {
				t.Fatal(err)
			}
			var handled bool
			expression := `(()=>{
const key=` + fmt.Sprintf("%q", key) + `,reverse=key==='ShiftTab';
if(reverse) window.paneItems[0].focus();
const event=new KeyboardEvent('keydown',{key:reverse?'Tab':key,shiftKey:reverse,bubbles:true,cancelable:true});
document.activeElement.dispatchEvent(event);
return event.defaultPrevented && (key==='Escape' || document.activeElement===window.paneItems[reverse?window.paneItems.length-1:0]);
})()`
			if err := chromedp.Run(ctx, chromedp.Evaluate(expression, &handled)); err != nil {
				t.Fatal(err)
			}
			if !handled {
				t.Fatalf("%s was not handled when :modal rejects", key)
			}
			if key == "Escape" {
				if err := chromedp.Run(ctx, chromedp.Poll(`!document.getElementById('rec-a')`, nil, chromedp.WithPollingTimeout(3*time.Second))); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
