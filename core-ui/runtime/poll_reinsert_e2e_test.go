package runtime

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A poll region that leaves the page and comes back polls again. The
// teardown that reclaims a detached region left its __fuiPollWired
// mark set, so re-inserting the same element (a region a layout moves,
// a node a host stashes and restores) was skipped by wireOne forever.
func TestPollRegionReinsertedPollsAgain(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<div id="host"><div id="region" data-cui-poll="5s" data-cui-poll-src="/fresh">stale</div></div>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, _ *http.Request) {
			n := hits.Add(1)
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "fresh %d", n)
		},
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr.loadedModules || {}).poll`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		// Detach, then let an unrelated insertion run the scanner's
		// reclaim pass, which tears the detached region down.
		chromedp.Evaluate(`window.__region = document.getElementById('region'); window.__region.remove(); true`, nil),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`document.body.appendChild(document.createElement('div')); true`, nil),
		chromedp.Sleep(50*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('host').appendChild(window.__region); true`, nil),
		chromedp.Poll(`document.getElementById('region').textContent.indexOf('fresh') === 0`, nil, chromedp.WithPollingTimeout(9*time.Second)),
	); err != nil {
		t.Fatalf("the re-inserted region never polled again (%d fetches): %v", hits.Load(), err)
	}
}

// A region the server stopped (#192, X-Gofastr-Poll-Stop) stays
// stopped through a later rescan: only a region that left the page is
// unmarked for re-wiring, so the reinsert fix above cannot restart a
// terminal poll still on the page.
func TestPollStoppedRegionStaysStoppedOnRescan(t *testing.T) {
	var hits atomic.Int32
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<div id="region" data-cui-poll="5s" data-cui-poll-src="/fresh">stale</div>
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/fresh": func(w http.ResponseWriter, _ *http.Request) {
			n := hits.Add(1)
			w.Header().Set("X-Gofastr-Poll-Stop", "1")
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprintf(w, "done %d", n)
		},
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('region').textContent === 'done 1'`, nil, chromedp.WithPollingTimeout(9*time.Second)),
		chromedp.Evaluate(`window.__gofastr._moduleScanners.poll(document); true`, nil),
		chromedp.Sleep(6500*time.Millisecond),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if n := hits.Load(); n != 1 {
		t.Fatalf("a stopped region fetched %d times after a rescan, want 1", n)
	}
}
