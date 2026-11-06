//go:build chromium

package sitesearch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

func pageUnavailable(t *testing.T, ctx context.Context, query, message string) {
	t.Helper()
	if err := chromedp.Run(ctx, chromedp.Poll(pageStatus+` === `+jsString(message), nil,
		chromedp.WithPollingTimeout(3*time.Second))); err != nil {
		var status string
		_ = chromedp.Run(ctx, chromedp.Evaluate(pageStatus, &status))
		t.Fatalf("failed query kept status %q instead of unavailable: %v", status, err)
	}
	var cleared bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('[data-search-page-list]').children.length === 0 &&
!document.querySelector('[data-search-page-pager]') &&
document.querySelector('[data-search-page]').getAttribute('data-search-page-query') === `+jsString(query)+` &&
`+pageInput+`.value === `+jsString(query)+` && location.search === '?q=' + encodeURIComponent(`+jsString(query)+`)`, &cleared)); err != nil {
		t.Fatal(err)
	}
	if !cleared {
		t.Fatal("unavailable search retained stale results, pager, query, or address")
	}
	if directory := os.Getenv("GOFASTR_SCREENSHOT_DIR"); directory != "" {
		var image []byte
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&image, 90)); err != nil {
			t.Fatal(err)
		}
		file, err := os.CreateTemp(directory, "sitesearch-error-*.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write(image); err != nil {
			file.Close()
			t.Fatal(err)
		}
		if err := file.Close(); err != nil {
			t.Fatal(err)
		}
		t.Logf("screenshot: %s", file.Name())
	}
}

func TestPageClearsFailedLiveSearch(t *testing.T) {
	for _, failure := range []string{"status", "transport", "markup", "translated"} {
		t.Run(failure, func(t *testing.T) {
			app, _ := searchSite(t, 25)
			handler := app.Router()
			message := reasonIndexUnavailable.English()
			catalog := i18n.NewMapCatalog()
			catalog.Set("fr", reasonIndexUnavailable.Key(), i18n.Message{Text: "Index indisponible"})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("X-Gofastr-Navigate") != "" && r.URL.Query().Get("q") == "valid" {
					if failure == "markup" {
						_, _ = w.Write([]byte("<main>not a search page</main>"))
					} else {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				if failure == "translated" {
					ctx := i18n.WithContext(r.Context(), i18n.Locale{Tag: "fr"})
					r = r.WithContext(i18n.WithTranslator(ctx, i18n.NewTranslator(catalog, "en")))
				}
				handler.ServeHTTP(w, r)
			}))
			t.Cleanup(server.Close)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(server.URL+"/search?q=walrus"),
				chromedp.Poll(`!!window.__gofastr?.loadedModules?.['sitesearch-page'] && !!document.querySelector('[data-search-page-pager]')`, nil),
			); err != nil {
				t.Fatal(err)
			}
			if failure == "transport" {
				if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr.fetch = function () { return Promise.reject(new Error('offline')); };`, nil)); err != nil {
					t.Fatal(err)
				}
			}
			if err := chromedp.Run(ctx, typeQuery("valid")); err != nil {
				t.Fatal(err)
			}
			if failure == "translated" {
				message = "Index indisponible"
			}
			pageUnavailable(t, ctx, "valid", message)
		})
	}
}

func TestPageReportsExportLoadFailure(t *testing.T) {
	for _, failure := range []string{"module", "manifest", "shard"} {
		t.Run(failure, func(t *testing.T) {
			var failing atomic.Bool
			failing.Store(true)
			server := staticPage(t, "/search", 1, func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					match := failure == "module" && r.URL.Path == "/sitesearch-static.js" ||
						failure == "manifest" && r.URL.Path == Root+"manifest.json" ||
						failure == "shard" && strings.HasPrefix(r.URL.Path, Root+"t-")
					if match && failing.Load() {
						w.WriteHeader(http.StatusServiceUnavailable)
						return
					}
					next.ServeHTTP(w, r)
				})
			})
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/search?q=form")); err != nil {
				t.Fatal(err)
			}
			pageUnavailable(t, ctx, "form", reasonIndexUnavailable.English())
			failing.Store(false)
			if err := chromedp.Run(ctx, typeQuery("form"), chromedp.Poll(pageTitles+` !== ''`, nil)); err != nil {
				t.Fatalf("retry: %v", err)
			}
		})
	}
}

func TestPageClearsFailedMore(t *testing.T) {
	var failing atomic.Bool
	server := staticPage(t, "/search", 1, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if failing.Load() && strings.HasPrefix(r.URL.Path, Root+"p") {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(server.URL+"/search?q=form"),
		chromedp.Poll(`!!document.querySelector('[data-search-page-pager] button')`, nil)); err != nil {
		t.Fatal(err)
	}
	failing.Store(true)
	if err := chromedp.Run(ctx, chromedp.Click(`[data-search-page-pager] button`, chromedp.ByQuery)); err != nil {
		t.Fatal(err)
	}
	pageUnavailable(t, ctx, "form", reasonIndexUnavailable.English())
}

func TestPageDropsStaleFailure(t *testing.T) {
	server := staticPage(t, "/search", 1)
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(server.URL+"/search?q=form"),
		chromedp.Poll(pageTitles+` !== ''`, nil),
		chromedp.Evaluate(`const find = window.__gofastr._sitesearch.staticFind;
window.__gofastr._sitesearch.staticFind = function (q, limit) {
  if (q === 'cafe') return new Promise(function (_, fail) { window.failOld = fail; });
  return find(q, limit);
};`, nil),
		typeQuery("cafe"),
		chromedp.Poll(`!!window.failOld`, nil),
		typeQuery("table"),
		chromedp.Poll(pageStatus+`.includes('“table”') && `+pageTitles+` !== ''`, nil),
		chromedp.Evaluate(`window.failOld(new Error('old query failed'));`, nil),
		chromedp.Evaluate(`new Promise(ok => requestAnimationFrame(() => ok(true)))`, nil),
	); err != nil {
		t.Fatal(err)
	}
	var kept bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(pageStatus+`.includes('“table”') && `+pageTitles+` !== '' && location.search === '?q=table'`, &kept)); err != nil {
		t.Fatal(err)
	}
	if !kept {
		t.Fatal("a stale failure cleared the newer results")
	}
}
