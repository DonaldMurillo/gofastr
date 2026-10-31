package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// TestComputedModule_NoEval is a CSP-safety guard: the computed reducer
// engine must never use eval/Function, reducers are real host functions
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
// the computed's consumer, no server round-trip.
func TestComputed_RecomputesOnDepChange(t *testing.T) {
	url := computedServer(t, `<script type="application/json" id="gofastr-signals">{"org.company":"Acme"}</script>`, `
  <span id="company" data-cui-signal="org.company">Acme</span>
  <h1 id="greeting" data-cui-signal="org.greeting" data-cui-computed="greet" data-cui-computed-deps="org.company"></h1>
  <button id="rename" data-cui-signal-set="org.company:Globex">rename</button>`,
		`R.greet = (v) => 'Hello ' + v['org.company'];`)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))

	var initial, afterRename string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
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

// computedServer serves a page with the runtime, the computed module,
// the given head and body, and a reducers script whose body assigns to R
// (window.__gofastr._reducers). Reducers load AFTER runtime.js, the
// WithExtraScripts order, so the namespace assignment cannot clobber
// them.
func computedServer(t *testing.T, head, body, reducers string) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := Module("computed")
	if !ok {
		t.Fatal("computed module not embedded")
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// The canonical demand-load URL, so the loader fetches it when it
	// sees [data-cui-computed].
	mux.HandleFunc("/__gofastr/runtime/computed.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(mod))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head>%s</head><body>%s
  <span id="ready">ready</span>
  <script src="/__gofastr/runtime.js"></script>
  <script src="/reducers.js"></script>
</body></html>`, head, body)
	})
	mux.HandleFunc("/reducers.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, "window.__gofastr = window.__gofastr || {};\nconst R = (window.__gofastr._reducers = window.__gofastr._reducers || {});\n"+reducers)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

// A cycle between computed nodes (a reads b, b reads a) recursed until
// the stack overflowed, re-running every reducer thousands of times
// (2026-10-27 R7). A node never recomputes inside its own recompute, so
// one write runs each reducer once.
func TestComputed_CycleStops(t *testing.T) {
	url := computedServer(t, `<script type="application/json" id="gofastr-signals">{"c.x":0}</script>`, `
  <span id="a" data-cui-signal="c.a" data-cui-computed="ra" data-cui-computed-deps="c.x,c.b"></span>
  <span id="b" data-cui-signal="c.b" data-cui-computed="rb" data-cui-computed-deps="c.a"></span>
  <button id="bump" data-cui-signal-set="c.x:1">bump</button>`,
		`window.__raCalls = 0;
R.ra = (v) => { window.__raCalls++; return String(v['c.x']) + 'a'; };
R.rb = (v) => String(v['c.a']) + 'b';`)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var before, after int
	var a, b string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('b').textContent === '0ab'`, nil),
		chromedp.Evaluate(`window.__raCalls`, &before),
		chromedp.Click(`#bump`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('b').textContent === '1ab'`, nil),
		chromedp.Evaluate(`window.__raCalls`, &after),
		chromedp.Text(`#a`, &a, chromedp.ByID),
		chromedp.Text(`#b`, &b, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if after-before != 1 {
		t.Errorf("one write ran reducer ra %d times, want 1", after-before)
	}
	if a != "1a" || b != "1ab" {
		t.Errorf("a, b = %q, %q, want 1a, 1ab", a, b)
	}
}
