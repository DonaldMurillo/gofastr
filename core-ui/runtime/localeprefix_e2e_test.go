package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// Locale prefixes (framework.WithLocalePrefixes): /es/b is the /b
// screen in Spanish. The route table holds /a and /b; the
// #gofastr-locales block names es as the one prefixed locale.
//
// Every screen renders "<path> N", N being that path's fetch count, so a
// repeat of N is a cache hit and a bump a re-fetch. window.__mark
// survives a soft navigation and dies with a document load.
func localePrefixSrv(t *testing.T) (*httptest.Server, func(string) (int, bool)) {
	t.Helper()
	return localePrefixSrvWith(t, "")
}

// localePrefixSrvWith puts outlet in each document's main, so a
// non-empty one hands navigation to the envelope module. /es/redir is
// a server policy redirect to the English /b. The rest are HTTP 303s,
// which fetch follows itself: /es/hop to /b from a route on another
// layout chain (the whole-page fetch), /es/jump to /b on the partial
// fetch, /es/stay to /es/b in this document, and /es/out and
// /es/hop-out to a page on another origin, one per leg.
func localePrefixSrvWith(t *testing.T, outlet string) (*httptest.Server, func(string) (int, bool)) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Readable cross-origin, preflight included, the worst case: the
		// runtime must load it as a document, never swap it into this
		// one. A target that refuses CORS fails the fetch instead.
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>o</title></head><body><main><h1 id="v">/x out</h1></main></body></html>`)
	}))
	t.Cleanup(other.Close)
	var mu sync.Mutex
	counts := map[string]int{}
	soft := map[string]bool{}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	content := func(path string, n int) string {
		return fmt.Sprintf(`<h1 id="v">%s %d</h1>`+
			`<a id="to-a" href="/es/a">a</a><a id="to-b" href="/es/b">b</a><a id="to-en" href="/b">en</a><a id="to-redir" href="/es/redir">r</a>`+
			`<a id="to-hop" href="/es/hop">h</a><a id="to-jump" href="/es/jump">j</a><a id="to-stay" href="/es/stay">s</a>`+
			`<a id="to-out" href="/es/out">o</a><a id="to-hop-out" href="/es/hop-out">ho</a>`+
			`<a id="to-doc" href="/es/doc">d</a><a id="to-hop-doc" href="/es/hop-doc">hd</a><a id="to-doc-bad" href="/es/doc-bad">db</a>`, path, n)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/es/redir":
			w.Header().Set("X-Gofastr-Location", "/b")
			w.WriteHeader(http.StatusOK)
			return
		case "/es/hop", "/es/jump":
			http.Redirect(w, r, "/b", http.StatusSeeOther)
			return
		case "/es/stay":
			http.Redirect(w, r, "/es/b", http.StatusSeeOther)
			return
		case "/es/out", "/es/hop-out":
			http.Redirect(w, r, other.URL+"/x", http.StatusSeeOther)
			return
		case "/es/doc", "/es/hop-doc":
			// uihost's answer to a policy sending the reader off the
			// app: a script fetch gets the URL named, a document load
			// the 303.
			if m := r.Header.Get("Sec-Fetch-Mode"); m == "cors" || m == "same-origin" {
				w.Header().Set("X-Gofastr-Document", other.URL+"/x")
				return
			}
			http.Redirect(w, r, other.URL+"/x", http.StatusSeeOther)
			return
		case "/es/doc-bad":
			w.Header().Set("X-Gofastr-Document", "javascript:window.__pwned=1")
			return
		case "/a", "/b", "/es/a", "/es/b":
		default:
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		counts[r.URL.Path]++
		n := counts[r.URL.Path]
		nav := r.Header.Get("X-Gofastr-Navigate") == "1"
		soft[r.URL.Path] = soft[r.URL.Path] || nav
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if nav {
			w.Header().Set("X-Gofastr-Partial", "true")
			fmt.Fprint(w, content(r.URL.Path, n))
			return
		}
		// The header outlives a soft navigation: a language switcher
		// built for the page that loaded the document, a plain hreflang
		// link and another site's alternate.
		page := strings.TrimPrefix(r.URL.Path, "/es")
		fmt.Fprint(w, `<!doctype html><html><head><title>t</title>`+
			`<script type="application/json" id="gofastr-routes">[{"path":"/a"},{"path":"/b"},{"path":"/redir"},{"path":"/hop","layouts":["l:other"]},`+
			`{"path":"/jump"},{"path":"/stay"},{"path":"/out"},{"path":"/hop-out","layouts":["l:other"]},`+
			`{"path":"/doc"},{"path":"/hop-doc","layouts":["l:other"]},{"path":"/doc-bad"}]</script>`+
			`<script type="application/json" id="gofastr-locales">{"default":"en","prefixes":["es"],"base":""}</script>`+
			`</head><body><header>`+
			`<a id="alt-en" rel="alternate" hreflang="en" href="`+page+`">English</a>`+
			`<a id="alt-es" rel="alternate" hreflang="ES" href="/es`+page+`">Español</a>`+
			`<a id="plain-es" hreflang="es" href="/es/a">Leer</a>`+
			`<a id="alt-de" rel="alternate" hreflang="de" href="https://de.example/a">Deutsch</a>`+
			// The rewriter writes a home link as /es, its match prefix
			// too; sec owns the /b section.
			`<a id="home" href="/es" data-cui-match-prefix="/es">Inicio</a>`+
			`<a id="home-bare" href="/es" data-cui-match-prefix>Inicio</a>`+
			`<a id="sec" href="/es/b/start" data-cui-match-prefix="/es/b">B</a>`+
			`</header><main role="main" tabindex="-1">`+outlet+content(r.URL.Path, n)+
			`</main><script src="/__gofastr/runtime.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(path string) (int, bool) {
		mu.Lock()
		defer mu.Unlock()
		return counts[path], soft[path]
	}
}

func TestPrefixedLinkNavigatesSoftly(t *testing.T) {
	srv, seen := localePrefixSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var mark bool
	var back string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Evaluate(`window.__mark = 1`, nil),
		chromedp.Click("#to-b", chromedp.ByID),
		waitText("#v", "/es/b 1"),
		chromedp.Evaluate(`window.__mark === 1`, &mark),
		chromedp.NavigateBack(),
		waitText("#v", "/es/a 1"),
		chromedp.Evaluate(`location.pathname`, &back),
	); err != nil {
		t.Fatal(err)
	}
	if _, nav := seen("/es/b"); !nav || !mark {
		t.Errorf("/es/a → /es/b loaded a document (navigate header %v, mark kept %v)", nav, mark)
	}
	if back != "/es/a" {
		t.Errorf("Back landed on %q", back)
	}
}

func TestLocaleSwitchLoadsDocument(t *testing.T) {
	srv, seen := localePrefixSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Evaluate(`window.__mark = 1`, nil),
		chromedp.Click("#to-en", chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	// A document load destroys the execution context a Poll runs in,
	// so retry the read until the new page answers.
	var mark bool
	for deadline := time.Now().Add(10 * time.Second); ; {
		var v string
		err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('#v')?.textContent ?? ''`, &v),
			chromedp.Evaluate(`window.__mark === 1`, &mark))
		if err == nil && v == "/b 1" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("/b never rendered: %q %v", v, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if _, nav := seen("/b"); nav || mark {
		t.Errorf("es → en swapped in place (navigate header %v, mark kept %v); a locale change is a document", nav, mark)
	}
}

// The navigators under test: a non-empty outlet hands navigation to the
// envelope module.
var localeNavigators = map[string]string{
	"plain":    "",
	"envelope": `<div data-cui-outlet="l:site#aside" id="aside">ASIDE</div>`,
}

// A server redirect from /es/redir to the English /b crosses into
// another locale, so it loads a document like a link there would. The
// redirect leg replaced the URL before checking, and the check then
// compared /b with itself and swapped English into the Spanish page.
// The 303s are followed inside fetch, where only the response URL says
// where the reader landed: /es/hop on the whole-page leg, /es/jump on
// the partial one. /es/stay lands in this locale and stays soft, and a
// redirect to another origin loads that origin's page on either leg.
func TestRedirectToLocaleLoadsDocument(t *testing.T) {
	cases := []struct {
		link, text string
		soft       bool
	}{
		{"to-redir", "/b ", false},
		{"to-hop", "/b ", false},
		{"to-jump", "/b ", false},
		{"to-stay", "/es/b ", true},
		{"to-out", "/x out", false},
		{"to-hop-out", "/x out", false},
		{"to-doc", "/x out", false},
		{"to-hop-doc", "/x out", false},
	}
	for name, outlet := range localeNavigators {
		for _, c := range cases {
			t.Run(name+"/"+c.link, func(t *testing.T) {
				redirectLands(t, outlet, c.link, c.text, c.soft)
			})
		}
	}
}

// X-Gofastr-Document names a page to load; anything but an http(s) URL
// fails the navigation instead of running.
func TestDocumentHeaderRefusesScript(t *testing.T) {
	for name, outlet := range localeNavigators {
		t.Run(name, func(t *testing.T) {
			srv, _ := localePrefixSrvWith(t, outlet)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var pwned, mark bool
			var path string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/es/a"),
				waitText("#v", "/es/a 1"),
				chromedp.Evaluate(`window.__mark = 1`, nil),
				chromedp.Click("#to-doc-bad", chromedp.ByID),
				chromedp.Sleep(800*time.Millisecond),
				chromedp.Evaluate(`window.__pwned === 1`, &pwned),
				chromedp.Evaluate(`window.__mark === 1`, &mark),
				chromedp.Evaluate(`location.pathname`, &path),
			); err != nil {
				t.Fatal(err)
			}
			if pwned || !mark || path != "/es/a" {
				t.Errorf("javascript: document header: ran %v, page kept %v, path %q", pwned, mark, path)
			}
		})
	}
}

func redirectLands(t *testing.T, outlet, link, text string, soft bool) {
	srv, _ := localePrefixSrvWith(t, outlet)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Evaluate(`window.__mark = 1`, nil),
		chromedp.Click("#"+link, chromedp.ByID),
	); err != nil {
		t.Fatal(err)
	}
	var mark bool
	var v, origin string
	for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		err := chromedp.Run(ctx,
			chromedp.Evaluate(`document.querySelector('#v')?.textContent ?? ''`, &v),
			chromedp.Evaluate(`location.origin`, &origin),
			chromedp.Evaluate(`window.__mark === 1`, &mark))
		if err == nil && strings.HasPrefix(v, text) {
			break
		}
		if time.Now().After(deadline) {
			var body string
			_ = chromedp.Run(ctx, chromedp.Evaluate(`location.href + ' ' + document.body.innerHTML.slice(0, 600)`, &body))
			t.Fatalf("%q never rendered: %q %v\n%s", text, v, err, body)
		}
	}
	if mark != soft {
		t.Errorf("%s landed on %q with mark kept %v, want a soft navigation %v", link, v, mark, soft)
	}
	if out := strings.HasPrefix(text, "/x"); out == (origin == srv.URL) {
		t.Errorf("%s landed on origin %s (this server %s)", link, origin, srv.URL)
	}
}

func TestSwitcherFollowsSoftNav(t *testing.T) {
	srv, _ := localePrefixSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	const read = `['alt-en','alt-es','plain-es','alt-de'].map((id) => document.getElementById(id).getAttribute('href')).join(' ')`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Click("#to-b", chromedp.ByID),
		waitText("#v", "/es/b 1"),
	); err != nil {
		t.Fatal(err)
	}
	const want = "/b /es/b /es/a https://de.example/a"
	var got string
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		if err := chromedp.Run(ctx, chromedp.Evaluate(read, &got)); err == nil && got == want {
			return
		}
	}
	t.Fatalf("after /es/a → /es/b the header links are %q, want %q", got, want)
}

// Each navigator keeps its own screen cache, keyed with the prefix; the
// server names screens without one.
func TestInvalidateMatchesPrefixedKey(t *testing.T) {
	for name, outlet := range localeNavigators {
		t.Run(name, func(t *testing.T) {
			srv, _ := localePrefixSrvWith(t, outlet)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/es/a"),
				waitText("#v", "/es/a 1"),
				chromedp.Click("#to-b", chromedp.ByID),
				waitText("#v", "/es/b 1"),
				chromedp.Click("#to-a", chromedp.ByID),
				waitText("#v", "/es/a 1"),
				chromedp.Evaluate(`__gofastr.invalidate('/b')`, nil),
				chromedp.Click("#to-b", chromedp.ByID),
				waitText("#v", "/es/b 2"),
			); err != nil {
				t.Fatalf("invalidate('/b') left the cached /es/b: %v", err)
			}
		})
	}
}

// A home link under a prefix is /es, which is no section: it must not
// light up on every Spanish page the way "/" never does.
func TestPrefixedHomeIsNoSection(t *testing.T) {
	srv, _ := localePrefixSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Click("#to-b", chromedp.ByID),
		waitText("#v", "/es/b 1"),
	); err != nil {
		t.Fatal(err)
	}
	const read = `['home','home-bare','sec'].map((id) => document.getElementById(id).getAttribute('aria-current') || '-').join(' ')`
	const want = "- - page"
	var got string
	for deadline := time.Now().Add(10 * time.Second); got != want && time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		_ = chromedp.Run(ctx, chromedp.Evaluate(read, &got))
	}
	if got != want {
		t.Fatalf("on /es/b home, bare home and the /b section read %q, want %q", got, want)
	}
	// A static export under /site writes the same links behind its base,
	// and /site is that export's root.
	const based = `window.__gofastr_locales.base = '/site';` +
		`for (const [id, h, m] of [['home', '/site/es', '/site/es'], ['home-bare', '/site/es/', null], ['sec', '/site/es/b/start', '/site/es/b']]) {` +
		` const a = document.getElementById(id); a.setAttribute('href', h); if (m) a.setAttribute('data-cui-match-prefix', m); a.removeAttribute('aria-current'); }` +
		`__gofastr._updateActiveLink('/site/es/b', true);`
	if err := chromedp.Run(ctx, chromedp.Evaluate(based, nil), chromedp.Evaluate(read, &got)); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("under base /site on /site/es/b the links read %q, want %q", got, want)
	}
}

func TestEndpointFetchesCarryPrefix(t *testing.T) {
	srv, _ := localePrefixSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var es, en []string
	const probe = `[__gofastr._lp('/__gofastr/action'), __gofastr._lp('/es/x'), __gofastr._lp('//h/x'),` +
		` __gofastr._lp('rel'), __gofastr._unlocalize('/es?q=1'), __gofastr._localeOf('/esx/y')]`
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Evaluate(probe, &es),
		chromedp.Navigate(srv.URL+"/a"),
		waitText("#v", "/a 1"),
		chromedp.Evaluate(probe, &en),
	); err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(es, " "), "/es/__gofastr/action /es/x //h/x rel /?q=1 "; got != want {
		t.Errorf("on /es/a: %q, want %q", got, want)
	}
	if got, want := strings.Join(en, " "), "/__gofastr/action /es/x //h/x rel /?q=1 "; got != want {
		t.Errorf("on /a: %q, want %q", got, want)
	}
}
