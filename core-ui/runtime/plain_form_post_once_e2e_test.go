package runtime

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A plain form the runtime intercepts (enctype="application/json", or
// data-cui-spa) and POSTs creates, exactly as a data-cui-rpc POST form
// does (TestFormPOSTResubmitInFlightSendsOnce). The plain bridge had no
// hold: a double click plus Enter sent three POSTs and the handler made
// three rows. A re-submit of the payload in flight is dropped and the
// form reads aria-busy while it waits; once the first answers, the next
// submit goes out. A changed payload submitted meanwhile is the newest
// value, so it follows the first. A PUT form replaces, so each submit
// still reaches the server.
func TestPlainFormPOSTResubmitInFlightSendsOnce(t *testing.T) {
	thrice := `(() => { const f = document.getElementById('f'); f.requestSubmit(); f.requestSubmit(); f.requestSubmit(); })()`
	for _, tc := range []struct {
		name, attrs string
		want        int32
	}{
		{"json post", `method="post" enctype="application/json"`, 1},
		{"spa post", `method="post" data-cui-spa`, 1},
		{"spa no method", `data-cui-spa`, 1},
		{"json put", `method="put" enctype="application/json"`, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, hits, hold := busyRig(t, `<form id="f" action="/rpc" `+tc.attrs+`><input id="title" name="title" value="x"><button type="submit">Save</button></form>`)
			ctx := chromedptest.Context(t)
			var busy bool
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL),
				chromedp.WaitReady(`#f`, chromedp.ByQuery),
				chromedp.Evaluate(thrice, nil),
				chromedp.Sleep(500*time.Millisecond),
				chromedp.Evaluate(`document.getElementById('f').getAttribute('aria-busy') === 'true'`, &busy),
			); err != nil {
				t.Fatal(err)
			}
			if n := hits.Load(); n != tc.want {
				t.Fatalf("three submits sent %d requests, want %d", n, tc.want)
			}
			if tc.want != 1 {
				return
			}
			if !busy {
				t.Error("the form is not aria-busy while its POST is in flight")
			}
			// A different payload while the first is out: queued, sent
			// when the first settles.
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`(() => { const f = document.getElementById('f'); document.getElementById('title').value = 'y'; f.requestSubmit(); })()`, nil),
				chromedp.Sleep(300*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			if n := hits.Load(); n != 1 {
				t.Fatalf("a changed submit while the first is in flight: %d requests, want it held behind the first", n)
			}
			close(hold)
			if err := chromedp.Run(ctx,
				chromedp.Poll(`!document.getElementById('f').hasAttribute('aria-busy')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				chromedp.Sleep(200*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			if n := hits.Load(); n != 2 {
				t.Fatalf("after the first POST settled: %d requests in total, want 2 (the first and the changed one)", n)
			}
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`document.getElementById('f').requestSubmit()`, nil),
				chromedp.Sleep(300*time.Millisecond),
			); err != nil {
				t.Fatal(err)
			}
			if n := hits.Load(); n != 3 {
				t.Fatalf("a submit after everything settled: %d requests in total, want 3", n)
			}
		})
	}
}
