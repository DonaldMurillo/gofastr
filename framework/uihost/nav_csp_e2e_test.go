package uihost

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// A navigation between two layout chains fetches the destination as a
// whole document and reads its shell out of it. That document's head
// carries inline sheets its own Content-Security-Policy header lists by
// hash, and the page doing the reading runs under the policy of the
// document the reader first loaded, which lists other hashes. Parsing
// the answer into a document made the browser judge each inline sheet
// against the live policy: one refused style-src report per sheet on
// every such navigation, in the console and at a report endpoint, for
// sheets the navigator never applies.
func TestCrossShellNavigationRaisesNoPolicyViolation(t *testing.T) {
	if testing.Short() {
		t.Skip("browser test")
	}
	// Two navigators read a whole document: the envelope module's, on a
	// page with an outlet, and core's own, on a page without one.
	for _, tc := range []struct {
		name, from, to, probe string
		envelope              bool
	}{
		{"envelope navigator", "/styled/b", "/other", "other", true},
		{"core navigator", "/other", "/styled/b", "b", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := ownedStyleServer(t)
			ctx := ownedStyleContext(t)
			var got struct {
				Path       string   `json:"path"`
				Soft       bool     `json:"soft"`
				Envelope   bool     `json:"envelope"`
				Styled     string   `json:"styled"`
				Violations []string `json:"violations"`
			}
			probe := `document.getElementById('` + tc.probe + `')`
			state := `({
				path: location.pathname,
				soft: window.__sameDocument === 1,
				envelope: performance.getEntriesByType('resource').some(e => e.name.includes('/runtime/envelope')),
				styled: ` + probe + ` ? getComputedStyle(` + probe + `).backgroundColor : '',
				violations: window.__v,
			})`
			if err := chromedp.Run(ctx,
				chromedp.ActionFunc(func(ctx context.Context) error {
					_, err := page.AddScriptToEvaluateOnNewDocument(`window.__v=[];document.addEventListener('securitypolicyviolation',e=>window.__v.push(e.violatedDirective+' '+e.blockedURI),true);`).Do(ctx)
					return err
				}),
				chromedp.Navigate(srv.URL+tc.from),
				chromedp.WaitReady(`main`, chromedp.ByQuery),
				chromedp.Poll(`!!(window.__gofastr && window.__gofastr.navigate) && document.readyState === 'complete'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(`window.__sameDocument = 1; window.__gofastr.navigate('`+tc.to+`')`, nil),
				chromedp.Poll(`!!`+probe+` && getComputedStyle(`+probe+`).backgroundColor === 'rgb(0, 128, 0)'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				// A report is queued as a task after the parse that raised it.
				chromedp.Sleep(200*time.Millisecond),
				chromedp.Evaluate(state, &got),
			); err != nil {
				t.Fatalf("navigate across shells: %v (state %+v)", err, got)
			}
			if got.Path != tc.to {
				t.Fatalf("landed on %q, want %s", got.Path, tc.to)
			}
			if !got.Soft {
				t.Fatal("the navigation loaded a new document: the test must cross shells in place")
			}
			if got.Envelope != tc.envelope {
				t.Fatalf("envelope module loaded leaving %s = %v, want %v: the arm must drive its own navigator", tc.from, got.Envelope, tc.envelope)
			}
			if got.Styled != "rgb(0, 128, 0)" {
				t.Errorf("the destination's own sheet did not apply: background %q", got.Styled)
			}
			if len(got.Violations) > 0 {
				t.Errorf("a navigation across shells raised policy violations: %q", got.Violations)
			}
		})
	}
}
