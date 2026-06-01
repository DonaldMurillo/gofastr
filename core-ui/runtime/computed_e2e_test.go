package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/chromedp/chromedp"
)

// TestComputedModule_NoEval is a CSP-safety guard: the computed reducer
// engine must never use eval/Function — reducers are real host functions
// registered by name, not strings.
func TestComputedModule_NoEval(t *testing.T) {
	src, err := os.ReadFile("src/computed.js")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`\beval\s*\(`),
		regexp.MustCompile(`new\s+Function\b`),
	} {
		if bad.Match(src) {
			t.Errorf("computed.js uses a dynamic-code sink matching %s", bad)
		}
	}
}

// TestComputed_RecomputesOnDepChange drives the full computed flow in a
// browser: a host-registered reducer derives a greeting from a dependency
// signal; mutating the dependency recomputes and fans the result out to
// the computed's consumer — no server round-trip.
func TestComputed_RecomputesOnDepChange(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("computed")
	if !ok {
		t.Fatal("computed module not embedded")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// Serve the computed module at the canonical demand-load URL so the
	// runtime's loader fetches it when it sees [data-fui-computed].
	mux.HandleFunc("/__gofastr/runtime/computed.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(mod))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head>
  <script type="application/json" id="gofastr-signals">{"org.company":"Acme"}</script>
</head><body>
  <span id="company" data-fui-signal="org.company">Acme</span>
  <h1 id="greeting" data-fui-signal="org.greeting" data-fui-computed="greet" data-fui-computed-deps="org.company"></h1>
  <button id="rename" data-fui-signal-set="org.company:Globex">rename</button>
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
  <!-- host registers reducers AFTER runtime.js (the WithExtraScripts
       order), so the namespace assignment can't clobber them -->
  <script src="/reducers.js"></script>
</body></html>`)
	})
	mux.HandleFunc("/reducers.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, `window.__gofastr = window.__gofastr || {};
(window.__gofastr._reducers = window.__gofastr._reducers || {}).greet = (v) => 'Hello ' + v['org.company'];`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)

	var initial, afterRename string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Initial compute on boot fills the empty computed node.
		chromedp.Poll(`document.getElementById('greeting').textContent === 'Hello Acme'`, nil),
		chromedp.Text(`#greeting`, &initial, chromedp.ByID),
		// Mutate the dependency client-side → recompute fans out.
		chromedp.Click(`#rename`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('greeting').textContent === 'Hello Globex'`, nil),
		chromedp.Text(`#greeting`, &afterRename, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if initial != "Hello Acme" {
		t.Errorf("initial computed = %q, want Hello Acme", initial)
	}
	if afterRename != "Hello Globex" {
		t.Errorf("computed did not recompute on dep change: %q, want Hello Globex", afterRename)
	}
}
