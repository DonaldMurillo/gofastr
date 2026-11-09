package main

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/framework/gallery"
)

// Each demo form posts to the gallery's demo-form route and is sent back
// to its demo with a 303.
func TestGalleryFormPostsSucceed(t *testing.T) {
	base := startE2EServer(t)
	for _, slug := range []string{"form", "formrepeater", "repeater", "conditionalfield"} {
		t.Run(slug, func(t *testing.T) {
			ctx := newE2EBrowserCtx(t)
			var posts sync.Map
			responses := make(chan int, 4)
			chromedp.ListenTarget(ctx, func(event any) {
				switch event := event.(type) {
				case *network.EventRequestWillBeSent:
					// The redirect's response arrives on the follow-up
					// request, which keeps the POST's id.
					if event.RedirectResponse != nil {
						if _, ok := posts.LoadAndDelete(event.RequestID); ok {
							responses <- int(event.RedirectResponse.Status)
						}
					}
					if event.Request.Method == "POST" && strings.HasPrefix(event.Request.URL, base+gallery.DemoFormPath+"?demo="+slug) {
						posts.Store(event.RequestID, true)
					}
				case *network.EventResponseReceived:
					if _, ok := posts.LoadAndDelete(event.RequestID); ok {
						responses <- int(event.Response.Status)
					}
				}
			})
			if err := chromedp.Run(ctx, network.Enable(), chromedp.Navigate(base+"/components/"+slug), pageReady(),
				chromedp.Evaluate(`(()=>{const form=document.querySelector('main #demo-conditional') || document.querySelector('main input[name="email"]')?.form || document.querySelector('main .fui-repeater__add')?.form;
if(!form){const alternate=document.querySelector('main .fui-form-repeater__add')?.form;if(!alternate)throw Error('demo form missing');return alternate.querySelector('.fui-form-repeater__add').click()}
form.querySelectorAll('input[type="email"]').forEach(input=>input.value='demo@example.test');
form.querySelectorAll('input[type="password"]').forEach(input=>input.value='example-password');
(form.querySelector('.fui-repeater__add')||form.querySelector('button[type="submit"]')).click()})()`, nil)); err != nil {
				t.Fatal(err)
			}
			select {
			case status := <-responses:
				if status != 303 {
					t.Fatalf("demo form answered %d, want 303 back to the demo", status)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("demo form sent no POST")
			}
		})
	}
}
