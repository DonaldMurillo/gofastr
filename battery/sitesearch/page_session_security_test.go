//go:build chromium

package sitesearch

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestPageKeepsFreshSession(t *testing.T) {
	for _, withMeta := range []bool{true, false} {
		name := "without_meta"
		if withMeta {
			name = "with_meta"
		}
		t.Run(name, func(t *testing.T) {
			app, _ := searchSite(t, 3)
			handler := app.Router()
			release := make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Gofastr-Navigate") != "" {
					switch r.URL.Query().Get("q") {
					case "val":
						select {
						case <-release:
						case <-r.Context().Done():
							return
						}
						w.Header().Set("X-Gofastr-Session", "stale")
					case "valid":
						w.Header().Set("X-Gofastr-Session", "fresh")
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			t.Cleanup(unblock)
			ctx := chromedptest.Context(t)
			metaSetup := `document.querySelector('meta[name="gofastr-sse"]')?.remove();`
			if withMeta {
				metaSetup += `document.head.appendChild(Object.assign(document.createElement('meta'), {name: 'gofastr-sse', content: '/__gofastr/sse?session=initial'}));`
			}
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/search"),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.['sitesearch-page']`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Evaluate(metaSetup+`window.invalidations = []; window.sent = []; window.received = [];
const invalidate = window.__gofastr.invalidate;
window.__gofastr.invalidate = function (path) { window.invalidations.push(path); return invalidate.call(this, path); };
const fetchPage = window.__gofastr.fetch;
window.__gofastr.fetch = async function (url, options) {
  const q = new URL(url, location.href).searchParams.get('q'); window.sent.push(q);
  const response = await fetchPage.call(this, url, options); window.received.push(q); return response;
};`, nil),
				typeQuery("val"),
				chromedp.Poll(`window.sent.includes('val')`, nil),
				typeQuery("valid"),
				chromedp.Poll(pageStatus+` === '1 result for “valid”'`, nil),
			); err != nil {
				t.Fatal(err)
			}
			unblock()
			var token string
			var invalidations []string
			if err := chromedp.Run(ctx,
				chromedp.Poll(`window.received.includes('val')`, nil),
				chromedp.Evaluate(`document.querySelector('meta[name="gofastr-sse"]')?.content || ''`, &token),
				chromedp.Evaluate(`window.invalidations`, &invalidations),
			); err != nil {
				t.Fatal(err)
			}
			if withMeta && token != "/__gofastr/sse?session=fresh" {
				t.Errorf("stream meta = %q, want fresh session", token)
			}
			if len(invalidations) != 1 || invalidations[0] != "*" {
				t.Errorf("cache invalidations = %v, want one current-session invalidation", invalidations)
			}
		})
	}
}
