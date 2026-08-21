package runtime

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// navPrefixPage renders a header whose links all opt into prefix matching.
// "/docs" is the canonical no-trailing-slash form most apps register;
// "/blog/" is the trailing-slash form; "/docs-old" shares a text prefix
// with /docs but is a different section and must never light up.
func navPrefixPage(path, js string) string {
	return fmt.Sprintf(`<!doctype html><html><head><title>%s</title></head><body>
  <nav aria-label="Primary">
    <a id="home" href="/" data-fui-match-prefix="">Home</a>
    <a id="docs" href="/docs" data-fui-match-prefix="">Documentation</a>
    <a id="blog" href="/blog/" data-fui-match-prefix="">Blog</a>
    <a id="docsold" href="/docs-old" data-fui-match-prefix="">Archive</a>
  </nav>
  <main><a id="deep" href="/docs/getting-started">Getting started</a> %s</main>
  <script src="/__gofastr/runtime.js"></script>
</body></html>`, path, path)
}

func navPrefixServer(t *testing.T) *httptest.Server {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// Highlighting lives in the idle-loaded activelink module now.
	mux.HandleFunc("/__gofastr/runtime/activelink.js", func(w http.ResponseWriter, _ *http.Request) {
		src, _ := Module("activelink")
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, src)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.Header.Get("X-Gofastr-Navigate") == "1" {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", r.URL.Path)
			fmt.Fprint(w, r.URL.Path)
			return
		}
		fmt.Fprint(w, navPrefixPage(r.URL.Path, js))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func navPrefixBrowser(t *testing.T) context.Context {
	t.Helper()
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.WSURLReadTimeout(90*time.Second),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	t.Cleanup(allocCancel)
	browserCtx, browserCancel := chromedp.NewContext(allocCtx)
	t.Cleanup(browserCancel)

	// chromedp starts Chrome lazily on the first Run: allocate against the
	// browser context so the browser's lifetime is the browser context's,
	// passing a timeout context here would make the browser die when that
	// deadline passed. The watchdog bounds only the startup wait.
	started := make(chan error, 1)
	go func() { started <- chromedp.Run(browserCtx) }()
	select {
	case err := <-started:
		if err != nil {
			t.Fatalf("chrome did not start: %v", err)
		}
	case <-time.After(90 * time.Second):
		t.Fatal("chrome did not start within 90s")
	}

	ctx, cancel := context.WithTimeout(browserCtx, 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// A canonical href with no trailing slash must light up on descendant
// routes. Apps register "/docs", not "/docs/", so requiring the trailing
// slash made MatchPrefix silently useless for the ordinary case.
func TestMatchPrefixCanonicalHrefOnDescendant(t *testing.T) {
	srv := navPrefixServer(t)
	ctx := navPrefixBrowser(t)

	var docs, blog, old, home string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/docs/getting-started"),
		chromedp.WaitVisible(`#docs`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('docs').getAttribute('aria-current')`, &docs),
		chromedp.Evaluate(`document.getElementById('blog').getAttribute('aria-current')`, &blog),
		chromedp.Evaluate(`document.getElementById('docsold').getAttribute('aria-current')`, &old),
		chromedp.Evaluate(`document.getElementById('home').getAttribute('aria-current')`, &home),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if docs != "page" {
		t.Errorf(`href="/docs" must be active on /docs/getting-started, got aria-current=%q`, docs)
	}
	if old == "page" {
		t.Errorf(`href="/docs-old" must not match /docs/getting-started — prefix matching is on path segments`)
	}
	if blog == "page" {
		t.Errorf(`href="/blog/" must not match /docs/getting-started, got aria-current=%q`, blog)
	}
	if home == "page" {
		t.Errorf(`href="/" must never be a prefix match, got aria-current=%q`, home)
	}
}

// The href's own page counts as a match, with or without the trailing
// slash on the href. Before, "/blog/" went dark on exactly /blog.
func TestMatchPrefixMatchesOwnPage(t *testing.T) {
	srv := navPrefixServer(t)
	ctx := navPrefixBrowser(t)

	var docs, blog string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/docs"),
		chromedp.WaitVisible(`#docs`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('docs').getAttribute('aria-current')`, &docs),
	); err != nil {
		t.Fatalf("chromedp /docs: %v", err)
	}
	if docs != "page" {
		t.Errorf(`href="/docs" must be active on /docs, got aria-current=%q`, docs)
	}

	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/blog"),
		chromedp.WaitVisible(`#blog`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('blog').getAttribute('aria-current')`, &blog),
	); err != nil {
		t.Fatalf("chromedp /blog: %v", err)
	}
	if blog != "page" {
		t.Errorf(`href="/blog/" must be active on /blog (its own canonical page), got aria-current=%q`, blog)
	}
}

// The other half of the slash mismatch: a trailing slash on the PATH
// against a slashless href. Servers that canonicalise the other way
// (redirecting /docs to /docs/) put the runtime in exactly this state.
func TestMatchPrefixTrailingSlashPath(t *testing.T) {
	srv := navPrefixServer(t)
	ctx := navPrefixBrowser(t)

	var docs, old string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/docs/"),
		chromedp.WaitVisible(`#docs`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('docs').getAttribute('aria-current')`, &docs),
		chromedp.Evaluate(`document.getElementById('docsold').getAttribute('aria-current')`, &old),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if docs != "page" {
		t.Errorf(`href="/docs" must be active on /docs/, got aria-current=%q`, docs)
	}
	if old == "page" {
		t.Errorf(`href="/docs-old" must not match /docs/`)
	}
}

// Client-side navigation runs the same matcher, so a descendant reached
// without a page load has to land in the same state as a cold load.
func TestMatchPrefixAfterClientNav(t *testing.T) {
	srv := navPrefixServer(t)
	ctx := navPrefixBrowser(t)

	var docs, path string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#deep`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		// #deep points at a descendant of /docs and is NOT itself a nav
		// item, so only the prefix rule can light #docs up.
		chromedp.Click(`#deep`, chromedp.ByID),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`document.getElementById('docs').getAttribute('aria-current')`, &docs),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if path != "/docs/getting-started" {
		t.Fatalf("client nav did not reach the descendant route, at %q", path)
	}
	if docs != "page" {
		t.Errorf(`href="/docs" must be active after client nav to /docs/getting-started, got aria-current=%q`, docs)
	}
}
