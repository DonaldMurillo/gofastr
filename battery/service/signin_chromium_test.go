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

	if err := chromedp.Run(ctx, chromedp.Navigate("file://"+page)); err != nil {
		t.Fatal(err)
	}
	// The chain navigates twice (the auto-POST, then the refresh). An
	// evaluation that lands mid-navigation fails with "Cannot find
	// context", and chromedp.Poll gives up on that, so each read here
	// is retried until the deadline instead.
	want := "http://" + d.addr + "/"
	var loc, body string
	deadline := time.Now().Add(20 * time.Second)
	for {
		var got struct{ Href, Body string }
		err := chromedp.Run(ctx, chromedp.Evaluate(
			`({Href: location.href, Body: document.body ? document.body.innerText : ""})`, &got))
		if err == nil {
			loc, body = got.Href, got.Body
			if loc == want && body != "" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("browser did not land on %s: at %q, body %q, last error %v", want, loc, body, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(body, "hello from svctest") {
		t.Fatalf("landed on %s without a session: %q", loc, body)
	}
}
