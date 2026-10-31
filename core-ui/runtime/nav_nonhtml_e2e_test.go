package runtime_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// nonHTMLServer: "/raw", "/rawp" and "/rawx" answer 200 text/markdown,
// the shape of a docs site's raw-markdown route under a catch-all page
// route. "/raw" shares the boot page's layout chain; "/rawx" sits on a
// chain the boot page lacks, so the cross-chain leg fetches it. envelope
// puts the boot page on a layout with an outlet, which loads the envelope
// module; "/plain" and "/rawp" declare no layouts, so a move between them
// after that module loaded takes its same-chain leg into a bare <main>.
func nonHTMLServer(t *testing.T, envelope bool) *httptest.Server {
	t.Helper()
	coreJS, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	main := `<main role="main" tabindex="-1" id="main"><p id="screen-home">HOME</p></main>`
	site := `[]`
	if envelope {
		main = `<div data-cui-layout="site" data-cui-layout-key="l:site">` +
			`<div data-cui-outlet="l:site#side" id="side">SIDE</div>` +
			`<main role="main" tabindex="-1" data-cui-layout-slot="l:site" id="main"><p id="screen-home">HOME</p></main></div>`
		site = `["l:site"]`
	}
	doc := func(main string) string {
		return `<!doctype html><html lang="en"><head><title>nonhtml</title>` +
			`<script type="application/json" id="gofastr-routes">[{"path":"/","layouts":` + site + `},{"path":"/raw","layouts":` + site + `},{"path":"/plain","layouts":[]},{"path":"/rawp","layouts":[]},{"path":"/rawx","layouts":["l:other"]}]</script>` +
			`</head><body><nav><a id="goRaw" href="/raw">raw</a><a id="goRawX" href="/rawx">rawx</a><a id="goPlain" href="/plain">plain</a><a id="goRawP" href="/rawp">rawp</a></nav>` +
			main + `<script src="/__gofastr/runtime.js"></script></body></html>`
	}
	page, plain := doc(main), doc(`<main role="main" tabindex="-1" id="main"><p id="screen-plain">PLAIN</p></main>`)
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(coreJS))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(req.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := runtime.Module(name)
		if !ok {
			http.NotFound(w, req)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(src))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/raw", "/rawp", "/rawx":
			w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
			fmt.Fprint(w, "# Raw\n\nnot a page\n")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if req.URL.Path == "/plain" {
			fmt.Fprint(w, plain)
			return
		}
		fmt.Fprint(w, page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// A 200 answer that is not HTML (2026-10-27 bug bash W4: a docs site's
// raw-markdown route under its catch-all) is refused like a non-HTML
// error status: the toast names it, the URL goes back and the page
// stays. The navigator swapped the parsed text's missing <main> in as ”
// and cached the blank screen.
func TestNavRefusesNonHTMLAnswer(t *testing.T) {
	cases := []struct{ via, link, keep, text, at string }{
		{"", "#goRaw", "screen-home", "HOME", "/"},
		{"", "#goRawX", "screen-home", "HOME", "/"},
		{"#goPlain", "#goRawP", "screen-plain", "PLAIN", "/plain"},
	}
	for _, envelope := range []bool{false, true} {
		srv := nonHTMLServer(t, envelope)
		for _, c := range cases {
			t.Run(fmt.Sprintf("envelope=%v/%s%s", envelope, c.via, c.link), func(t *testing.T) {
				ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
				acts := []chromedp.Action{
					chromedp.Navigate(srv.URL + "/"),
					chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
				}
				if c.via != "" {
					acts = append(acts, chromedp.Click(c.via, chromedp.ByQuery), chromedp.WaitVisible(`#`+c.keep, chromedp.ByID))
				}
				var kept, path string
				var soft bool
				acts = append(acts,
					chromedp.Evaluate(`window.__softMark = 1; true`, nil),
					chromedp.Click(c.link, chromedp.ByQuery),
					chromedp.Sleep(1500*time.Millisecond),
					chromedp.Evaluate(`(document.getElementById('`+c.keep+`') || {}).textContent || ''`, &kept),
					chromedp.Evaluate(`location.pathname`, &path),
					chromedp.Evaluate(`window.__softMark === 1`, &soft),
				)
				if err := chromedp.Run(ctx, acts...); err != nil {
					t.Fatalf("chromedp: %v", err)
				}
				if !soft {
					t.Fatal("the navigation reloaded the document")
				}
				if kept != c.text || path != c.at {
					t.Fatalf("after a text/markdown answer: %s %q at %s, want the page kept at %s", c.keep, kept, path, c.at)
				}
			})
		}
	}
}
