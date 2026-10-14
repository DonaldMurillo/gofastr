//go:build chromium

package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// The sign-in chain `open` relies on, in a real browser: a file://
// page auto-POSTs the token, the login answer sets a SameSite=Strict
// cookie and refreshes to "/", and that refresh carries the cookie. A
// 303 in place of the refresh would arrive at "/" without it, because
// a redirect chain that began on a file:// page is cross-site.
func TestSignInPageLandsSignedIn(t *testing.T) {
	d := startDevRun(t)
	if err := d.b.openWith(d.l, false); err != nil {
		t.Fatal(err)
	}
	calls := d.ft.got()
	if len(calls) != 1 {
		t.Fatalf("launch calls %q", calls)
	}
	fields := strings.Fields(calls[0])
	page := fields[len(fields)-1]

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox)...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var body, loc string
	err := chromedp.Run(ctx,
		chromedp.Navigate("file://"+page),
		chromedp.Poll(`location.href === "http://`+d.addr+`/" && document.body && document.body.innerText.length > 0`, nil,
			chromedp.WithPollingTimeout(20*time.Second)),
		chromedp.Location(&loc),
		chromedp.Text("body", &body, chromedp.ByQuery),
	)
	if err != nil {
		t.Fatalf("browser did not land on the app: %v (at %q, body %q)", err, loc, body)
	}
	if !strings.Contains(body, "hello from svctest") {
		t.Fatalf("landed on %s without a session: %q", loc, body)
	}
}
