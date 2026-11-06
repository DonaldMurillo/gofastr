//go:build chromium

package sitesearch

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func TestPageFollowsPolicyRedirects(t *testing.T) {
	for _, mode := range []string{"location", "redirect", "document"} {
		t.Run(mode, func(t *testing.T) {
			app, _ := searchSite(t, 1)
			handler := app.Router()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Gofastr-Navigate") != "" && r.URL.Path == "/search" {
					target := "/docs/forms?source=" + mode
					switch mode {
					case "location":
						w.Header().Set("X-Gofastr-Location", target)
					case "redirect":
						http.Redirect(w, r, target, http.StatusSeeOther)
					case "document":
						w.Header().Set("X-Gofastr-Document", "http://"+r.Host+target)
					}
					return
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			ctx := chromedptest.Context(t, chromedptest.Timeout(10*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/search"),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.['sitesearch-page']`, nil),
				chromedp.Evaluate(`window.searchDocument = true`, nil),
				typeQuery("valid"),
				chromedp.WaitVisible(`//h1[normalize-space()='Forms']`, chromedp.BySearch),
				chromedp.Poll(`location.pathname === '/docs/forms' && !document.querySelector('[data-search-page]')`, nil,
					chromedp.WithPollingTimeout(4*time.Second)),
			); err != nil {
				t.Fatalf("search policy did not navigate: %v", err)
			}
			var sameDocument bool
			var query string
			if err := chromedp.Run(ctx,
				chromedp.Evaluate(`window.searchDocument === true`, &sameDocument),
				chromedp.Evaluate(`location.search`, &query),
			); err != nil {
				t.Fatal(err)
			}
			if sameDocument != (mode != "document") || query != "?source="+mode {
				t.Fatalf("policy result: same document=%v, query=%q", sameDocument, query)
			}
		})
	}
}
