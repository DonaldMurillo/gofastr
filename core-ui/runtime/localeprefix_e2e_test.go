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
// a server policy redirect to the English /b.
func localePrefixSrvWith(t *testing.T, outlet string) (*httptest.Server, func(string) (int, bool)) {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
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
			`<a id="to-a" href="/es/a">a</a><a id="to-b" href="/es/b">b</a><a id="to-en" href="/b">en</a><a id="to-redir" href="/es/redir">r</a>`, path, n)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/es/redir":
			w.Header().Set("X-Gofastr-Location", "/b")
			w.WriteHeader(http.StatusOK)
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
			`<script type="application/json" id="gofastr-routes">[{"path":"/a"},{"path":"/b"},{"path":"/redir"}]</script>`+
			`<script type="application/json" id="gofastr-locales">{"default":"en","prefixes":["es"],"base":""}</script>`+
			`</head><body><header>`+
			`<a id="alt-en" rel="alternate" hreflang="en" href="`+page+`">English</a>`+
			`<a id="alt-es" rel="alternate" hreflang="ES" href="/es`+page+`">Español</a>`+
			`<a id="plain-es" hreflang="es" href="/es/a">Leer</a>`+
			`<a id="alt-de" rel="alternate" hreflang="de" href="https://de.example/a">Deutsch</a>`+
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

// A server redirect from /es/redir to the English /b crosses into
// another locale, so it loads a document like a link there would. The
// redirect leg replaced the URL before checking, and the check then
// compared /b with itself and swapped English into the Spanish page.
func TestRedirectToLocaleLoadsDocument(t *testing.T) {
	for name, outlet := range map[string]string{
		"plain":    "",
		"envelope": `<div data-cui-outlet="l:site#aside" id="aside">ASIDE</div>`,
	} {
		t.Run(name, func(t *testing.T) {
			srv, seen := localePrefixSrvWith(t, outlet)
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/es/a"),
				waitText("#v", "/es/a 1"),
				chromedp.Evaluate(`window.__mark = 1`, nil),
				chromedp.Click("#to-redir", chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			var mark bool
			var v, path string
			for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(50 * time.Millisecond) {
				err := chromedp.Run(ctx,
					chromedp.Evaluate(`document.querySelector('#v')?.textContent ?? ''`, &v),
					chromedp.Evaluate(`location.pathname`, &path),
					chromedp.Evaluate(`window.__mark === 1`, &mark))
				if err == nil && strings.HasPrefix(v, "/b ") {
					break
				}
				if time.Now().After(deadline) {
					var body string
					_ = chromedp.Run(ctx, chromedp.Evaluate(`location.href + ' ' + document.body.innerHTML.slice(0, 600)`, &body))
					t.Fatalf("/b never rendered: %q %v\n%s", v, err, body)
				}
			}
			if _, nav := seen("/b"); nav || mark || path != "/b" {
				t.Errorf("es → en redirect swapped in place (navigate header %v, mark kept %v, path %q)", nav, mark, path)
			}
		})
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

func TestInvalidateMatchesPrefixedKey(t *testing.T) {
	srv, _ := localePrefixSrv(t)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/es/a"),
		waitText("#v", "/es/a 1"),
		chromedp.Click("#to-b", chromedp.ByID),
		waitText("#v", "/es/b 1"),
		chromedp.Click("#to-a", chromedp.ByID),
		waitText("#v", "/es/a 1"),
		// The server names screens without a prefix.
		chromedp.Evaluate(`__gofastr.invalidate('/b')`, nil),
		chromedp.Click("#to-b", chromedp.ByID),
		waitText("#v", "/es/b 2"),
	); err != nil {
		t.Fatalf("invalidate('/b') left the cached /es/b: %v", err)
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
