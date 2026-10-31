package runtime_test

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// The anchor store holds 50 pages. Eviction sorted the paths and dropped
// the first, so the page just left lost its Back-restore anchor to 50
// stale paths that sort after it. The least recently recorded go first,
// and recording a page again makes it the most recent: "/" is seeded
// oldest of a full store, left, then "/a" is recorded new on Back.
func TestScrollAnchorsEvictOldest(t *testing.T) {
	srv := leaveCaptureServer(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	seed := `(() => { const o = {'/': {a: null, e: {}}}; for (let i = 0; i < 49; i++) o['/z' + String(i).padStart(2, '0')] = {a: null, e: {}};
		sessionStorage.setItem('gofastr:anchors', JSON.stringify(o)); return true; })()`
	var kept string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#rb`, chromedp.ByID),
		chromedp.Evaluate(seed, nil),
		chromedp.Reload(),
		chromedp.WaitVisible(`#rb`, chromedp.ByID),
		chromedp.Click(`#goA`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-a`, chromedp.ByID),
		chromedp.Evaluate(`history.back()`, nil),
		chromedp.WaitVisible(`#rb`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`(() => { const o = JSON.parse(sessionStorage.getItem('gofastr:anchors') || '{}');
			return JSON.stringify({n: Object.keys(o).length, home: '/' in o, a: '/a' in o, z00: '/z00' in o}); })()`, &kept),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if kept != `{"n":50,"home":true,"a":true,"z00":false}` {
		t.Fatalf("anchors after Back: %s, want / and /a kept and /z00 evicted", kept)
	}
}
