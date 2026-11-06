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

func TestPageUsesKernelMarkup(t *testing.T) {
	for _, markup := range []string{"", "2", "future-9"} {
		name := markup
		if name == "" {
			name = "older kernel"
		}
		t.Run(name, func(t *testing.T) {
			app, _ := searchSite(t, 3)
			handler := app.Router()
			received := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Gofastr-Navigate") != "" {
					select {
					case received <- r.Header.Get("X-Gofastr-Markup"):
					default:
					}
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/search"),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.['sitesearch-page']`, nil),
				chromedp.Evaluate(`window.__gofastr._markup = `+jsString(markup)+`;`, nil),
				typeQuery("valid"),
			); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-received:
				if got != markup {
					t.Fatalf("markup header = %q, live kernel supports %q", got, markup)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("typing never fetched search results")
			}
		})
	}
}
