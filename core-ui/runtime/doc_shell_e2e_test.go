package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chromedp/chromedp"
)

// docShellSite exercises the document-shell contract (#408 + #411): the
// outermost layout layer carries data-fui-doc-lang and data-fui-skip-label,
// and after a client-side swap the runtime copies them onto
// documentElement.lang and the skip link. Two shapes:
//
//	/en/x, /es/x      → keyed single-layer shells ["l:docs-en"] / ["l:docs-es"]
//	/g/en/x, /g/es/x  → shared root ["l:site", "g:/g/en/:docs"] / ["l:site", "g:/g/es/:docs"]
//
// The first must replace the shell node on a language switch (WithKey
// per language); the second must keep the shared shell node while the
// swapped payload still updates lang and the skip link.
type docShellSite struct {
	srv  *httptest.Server
	mu   sync.Mutex
	full map[string]int
	part map[string]int
}

func (c *docShellSite) record(path string, partial bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if partial {
		c.part[path]++
	} else {
		c.full[path]++
	}
}

const docShellRoutes = `<script type="application/json" id="gofastr-routes">[` +
	`{"path":"/en/x","layouts":["l:docs-en"]},` +
	`{"path":"/es/x","layouts":["l:docs-es"]},` +
	`{"path":"/g/en/x","layouts":["l:site","g:/g/en/:docs"]},` +
	`{"path":"/g/es/x","layouts":["l:site","g:/g/es/:docs"]}` +
	`]</script>`

func docShellBody(lang, label string) string {
	return `<a href="#main-content" class="skip-link" data-skip-link>` + label + `</a>`
}

// keyedPage: one layout name ("docs"), one layer, per-language key. This is
// the #408 shape: identity varies by language, the CSS contract does not.
func keyedPage(lang, key, label, id, linkID, linkHref, linkText string) string {
	return `<!doctype html><html lang="` + lang + `"><head><title>` + id + `</title>` + docShellRoutes + `</head><body>` +
		docShellBody(lang, label) +
		`<div data-fui-layout="docs" data-fui-layout-key="` + key + `" data-fui-doc-lang="` + lang + `" data-fui-skip-label="` + label + `" class="layout-docs">` +
		`<main role="main" tabindex="-1" data-fui-layout-slot="` + key + `">` +
		`<h1 id="` + id + `">` + id + `</h1><a id="` + linkID + `" href="` + linkHref + `">` + linkText + `</a>` +
		`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
}

// sharedRootPage: the site shell (l:site) is layer 0 and carries the doc
// markers on a full render; a partial to the other language re-renders only
// the group layer, which then carries the fresh markers.
func sharedRootPage(lang, groupPrefix, groupKey, label, id, linkID, linkText string) string {
	return `<!doctype html><html lang="` + lang + `"><head><title>` + id + `</title>` + docShellRoutes + `</head><body>` +
		docShellBody(lang, label) +
		`<div data-fui-layout="site" data-fui-layout-key="l:site" data-fui-doc-lang="` + lang + `" data-fui-skip-label="` + label + `" class="layout-site">` +
		`<header id="site-header">` + lang + `</header>` +
		`<main role="main" tabindex="-1" data-fui-layout-slot="l:site">` +
		`<div class="fui-screen-group" data-fui-screen-group="` + groupPrefix + `">` +
		`<div data-fui-layout="docs" data-fui-layout-key="` + groupKey + `" class="layout-docs">` +
		`<div class="layout-content" tabindex="-1" data-fui-layout-slot="` + groupKey + `">` +
		`<h1 id="` + id + `">` + id + `</h1><a id="` + linkID + `" href="/g/es/x">` + linkText + `</a>` +
		`</div></div></div>` +
		`</main></div><script src="/__gofastr/runtime.js"></script></body></html>`
}

// sharedRootPartial: the fragment the server sends for /g/en/x → /g/es/x:
// only the group layer re-renders, and it carries the fresh markers.
func sharedRootPartial(groupPrefix, groupKey string) string {
	return `<div class="fui-screen-group" data-fui-screen-group="` + groupPrefix + `">` +
		`<div data-fui-layout="docs" data-fui-layout-key="` + groupKey + `" data-fui-doc-lang="es" data-fui-skip-label="Saltar al contenido principal" class="layout-docs">` +
		`<div class="layout-content" tabindex="-1" data-fui-layout-slot="` + groupKey + `">` +
		`<h1 id="g-es-screen">g-es-screen</h1>` +
		`</div></div></div>`
}

const (
	skipEN = "Skip to main content"
	skipES = "Saltar al contenido principal"
)

func newDocShellSite(t *testing.T) *docShellSite {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	c := &docShellSite{full: map[string]int{}, part: map[string]int{}}

	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	// Keyed single-layer shells: disjoint chains, the runtime must
	// full-fetch and replace the shell (never a partial).
	mux.HandleFunc("/en/x", func(w http.ResponseWriter, _ *http.Request) {
		c.record("/en/x", false)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, keyedPage("en", "l:docs-en", skipEN, "en-screen", "to-es", "/es/x", "ES"))
	})
	mux.HandleFunc("/es/x", func(w http.ResponseWriter, _ *http.Request) {
		c.record("/es/x", false)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, keyedPage("es", "l:docs-es", skipES, "es-screen", "to-en", "/en/x", "EN"))
	})
	mux.HandleFunc("/g/en/x", func(w http.ResponseWriter, r *http.Request) {
		partial := r.Header.Get("X-Gofastr-Navigate") == "1"
		c.record("/g/en/x", partial)
		w.Header().Set("Content-Type", "text/html")
		if partial {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "g-es")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, sharedRootPartial("/g/es/", "g:/g/es/:docs"))
			return
		}
		fmt.Fprint(w, sharedRootPage("en", "/g/en/", "g:/g/en/:docs", skipEN, "g-en-screen", "g-to-es", "ES"))
	})
	mux.HandleFunc("/g/es/x", func(w http.ResponseWriter, r *http.Request) {
		partial := r.Header.Get("X-Gofastr-Navigate") == "1"
		c.record("/g/es/x", partial)
		w.Header().Set("Content-Type", "text/html")
		if partial {
			w.Header().Set("X-Gofastr-Partial", "true")
			w.Header().Set("X-Gofastr-Title", "g-en")
			w.Header().Set("X-Gofastr-Swap", "l:site")
			fmt.Fprint(w, sharedRootPartial("/g/en/", "g:/g/en/:docs"))
			return
		}
		fmt.Fprint(w, sharedRootPage("es", "/g/es/", "g:/g/es/:docs", skipES, "g-es-screen", "g-to-en", "EN"))
	})

	c.srv = httptest.NewServer(mux)
	t.Cleanup(c.srv.Close)
	return c
}

// A per-language keyed shell (WithKey) makes a language switch a
// cross-chain navigation: the shell element is replaced, and the fresh
// doc markers on the new shell update <html lang> and the skip link
// without a hard reload.
func TestKeyedShellSwapSyncsDocLangAndSkip(t *testing.T) {
	site := newDocShellSite(t)
	ctx := newSeedBrowserCtx(t)

	var lang, skip, result string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/en/x"),
		chromedp.WaitVisible(`#en-screen`, chromedp.ByID),
		chromedp.Evaluate(`window.__survived = true;
			document.querySelector('[data-fui-layout-key]').dataset.stamp = 'old';
			document.documentElement.lang`, &lang),
		chromedp.Evaluate(`document.querySelector('[data-skip-link]').textContent`, &skip),
		chromedp.Click(`#to-es`, chromedp.ByID),
		chromedp.WaitVisible(`#es-screen`, chromedp.ByID),
		chromedp.Evaluate(`[
			document.documentElement.lang,
			document.querySelector('[data-skip-link]').textContent,
			window.__survived === true,
			document.querySelector('[data-fui-layout-key="l:docs-en"]') === null,
			!!document.querySelector('[data-fui-layout-key="l:docs-es"][data-fui-doc-lang="es"]'),
		].join('|')`, &result),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if lang != "en" || skip != skipEN {
		t.Fatalf("initial page: lang=%q skip=%s, want en/%s", lang, skip, skipEN)
	}
	parts := strings.Split(result, "|")
	if len(parts) != 5 {
		t.Fatalf("probe returned %q", result)
	}
	gotLang, gotSkip, noReload, gone, there := parts[0], parts[1], parts[2] == "true", parts[3] == "true", parts[4] == "true"
	if gotLang != "es" {
		t.Errorf("documentElement.lang = %q after /es/x, want es", gotLang)
	}
	if gotSkip != skipES {
		t.Errorf("skip link = %q after /es/x, want %q", gotSkip, skipES)
	}
	if !noReload {
		t.Error("navigation hard-reloaded the document (window flag lost)")
	}
	if !gone || !there {
		t.Errorf("keyed shell must be replaced: old gone=%v new present=%v", gone, there)
	}
	if site.part["/es/x"] != 0 {
		t.Errorf("cross-chain language nav used %d partial fetches; must full-fetch", site.part["/es/x"])
	}
}

// Same outer key on both sides keeps the shell node (the existing swap
// behavior) while the swapped payload's markers still update the
// document language and skip label.
func TestSharedShellKeepsNodeSyncsDocLang(t *testing.T) {
	site := newDocShellSite(t)
	ctx := newSeedBrowserCtx(t)

	var result string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(site.srv.URL+"/g/en/x"),
		chromedp.WaitVisible(`#g-en-screen`, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('site-header').dataset.stamp = 'kept'`, nil),
		chromedp.Click(`#g-to-es`, chromedp.ByID),
		chromedp.WaitVisible(`#g-es-screen`, chromedp.ByID),
		chromedp.Evaluate(`[
			document.documentElement.lang,
			document.querySelector('[data-skip-link]').textContent,
			document.getElementById('site-header').dataset.stamp === 'kept',
		].join('|')`, &result),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	parts := strings.Split(result, "|")
	if len(parts) != 3 {
		t.Fatalf("probe returned %q", result)
	}
	if parts[0] != "es" {
		t.Errorf("documentElement.lang = %q, want es (markers must update on an in-chain swap)", parts[0])
	}
	if parts[1] != skipES {
		t.Errorf("skip link = %q, want %q", parts[1], skipES)
	}
	if parts[2] != "true" {
		t.Error("shared shell node was rebuilt although the root layer matches")
	}
	if site.part["/g/es/x"] == 0 {
		t.Error("in-chain language nav did not use a partial fetch")
	}
}
