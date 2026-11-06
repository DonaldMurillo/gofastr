package runtime

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestFullNavSessionClearsCache(t *testing.T) {
	for name, marker := range map[string]string{"plain": "", "envelope": `<div data-cui-outlet="side"></div>`} {
		for _, changed := range []bool{false, true} {
			suffix, session := "same", "old-id"
			if changed {
				suffix, session = "changed", "rotated%2Bidentity"
			}
			t.Run(name+"/"+suffix, func(t *testing.T) {
				routes := `[{"path":"/"},{"path":"/a"},{"path":"/bridge","layouts":["l:site"]},{"path":"/b"}]`
				oldStream := "/__gofastr/sse?session=old-id&locale=en"
				newStream := "/__gofastr/sse?locale=fr&session=" + session
				site := newNavGapSite(t, routes, map[string]navGapPage{
					"/":       {stream: oldStream, inner: okPage(`<a id="to-a" href="/a">A</a>` + marker)},
					"/a":      {stream: oldStream, inner: okPage(`<a id="to-bridge" href="/bridge">Bridge</a>` + marker)},
					"/bridge": {shell: true, stream: oldStream, inner: okPage(`<a id="to-b" href="/b">B</a>`)},
					"/b":      {stream: newStream, inner: okPage(`<p id="new-page">New identity</p>`)},
				})
				ctx := chromedptest.Context(t)
				if err := chromedp.Run(ctx,
					chromedp.Navigate(site.srv.URL+"/"),
					chromedp.Poll(`!!window.__gofastr?.navigate`, nil),
				); err != nil {
					t.Fatal(err)
				}
				if marker != "" {
					if err := chromedp.Run(ctx,
						chromedp.Evaluate(`window.__gofastr.loadModule('envelope')`, nil),
						chromedp.Poll(`!!window.__gofastr.loadedModules.envelope`, nil),
					); err != nil {
						t.Fatal(err)
					}
				}
				var stream string
				if err := chromedp.Run(ctx,
					chromedp.Evaluate(`document.getElementById('to-a').click()`, nil),
					chromedp.WaitVisible(`#to-bridge`, chromedp.ByID),
					chromedp.Evaluate(`document.getElementById('to-bridge').click()`, nil),
					chromedp.WaitVisible(`#to-b`, chromedp.ByID),
					chromedp.Evaluate(`document.getElementById('to-b').click()`, nil),
					chromedp.WaitVisible(`#new-page`, chromedp.ByID),
					chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]').content`, &stream),
					chromedp.Evaluate(`history.go(-2)`, nil),
					chromedp.WaitVisible(`#to-bridge`, chromedp.ByID),
					chromedp.Poll(`location.pathname === '/a'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
				); err != nil {
					t.Fatal(err)
				}
				if stream != newStream {
					t.Errorf("stream metadata = %q, want %q", stream, newStream)
				}
				want := 1
				if changed {
					want = 2
				}
				if got := site.count("/a"); got != want {
					t.Errorf("Back fetched the old page %d times, want %d for session changed=%v", got, want, changed)
				}
			})
		}
	}
}
