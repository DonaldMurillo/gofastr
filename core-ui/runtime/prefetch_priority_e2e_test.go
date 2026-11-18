package runtime

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// An eager prefetch is speculative and asks for a low fetch priority; a
// hover prefetch is about to become a click and keeps the default.
// Chrome reports the hint as the request's scheduler priority.
func TestPrefetchPriorityFollowsIntent(t *testing.T) {
	srv, _ := newPreloadSite(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var mu sync.Mutex
	prio := map[string]string{}
	chromedp.ListenTarget(ctx, func(ev any) {
		e, ok := ev.(*network.EventRequestWillBeSent)
		if !ok {
			return
		}
		if v, _ := e.Request.Headers["X-Gofastr-Prefetch"].(string); v != "1" {
			return
		}
		u, err := url.Parse(e.Request.URL)
		if err != nil {
			return
		}
		mu.Lock()
		prio[u.Path] = string(e.Request.InitialPriority)
		mu.Unlock()
	})
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(srv.URL+"/hub"),
		chromedp.WaitVisible(`#hub-screen`, chromedp.ByID),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('to-detail').dispatchEvent(new PointerEvent('pointerover', {bubbles: true}))`, nil),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	eager, hover := prio["/eager"], prio["/detail"]
	if eager == "" || hover == "" {
		t.Fatalf("missing prefetches: %v", prio)
	}
	if !strings.Contains(eager, "Low") {
		t.Errorf("eager prefetch priority = %q, want Low or VeryLow", eager)
	}
	if strings.Contains(hover, "Low") {
		t.Errorf("hover prefetch priority = %q; a hover is about to be a click and must not yield", hover)
	}
}

// A poll is background freshness: its fetch asks for a low priority so
// it never contends with what the user is waiting on.
func TestPollFetchIsLowPriority(t *testing.T) {
	page := `<!doctype html><html><head></head><body>
<div id="region" data-cui-poll="5s" data-cui-poll-src="/fresh">stale</div>
<script src="/__gofastr/runtime.js"></script></body></html>`
	base := startPollServer(t, page, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<span>fresh</span>`)
		},
	})
	ctx := chromedptest.Context(t)
	var mu sync.Mutex
	var got string
	chromedp.ListenTarget(ctx, func(ev any) {
		if e, ok := ev.(*network.EventRequestWillBeSent); ok && strings.HasSuffix(e.Request.URL, "/fresh") {
			mu.Lock()
			got = string(e.Request.InitialPriority)
			mu.Unlock()
		}
	})
	if err := chromedp.Run(ctx,
		network.Enable(),
		chromedp.Navigate(base+"/"),
		chromedp.Poll(`document.getElementById('region').textContent === 'fresh'`, nil,
			chromedp.WithPollingInterval(200*time.Millisecond)),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(got, "Low") {
		t.Errorf("poll fetch priority = %q, want Low or VeryLow", got)
	}
}
