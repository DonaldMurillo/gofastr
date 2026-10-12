package runtime_test

// Version-skew e2e across the data-fui-* → data-cui-* rename (#467):
// a tab still running the v0.86.0 runtime keeps intercepting links
// after the upgrade, because its click handler checks only a[href],
// the route manifest and the opt-outs. Swapping today's data-cui-*
// markup into that kernel leaves every interactive marker unread: a
// data-cui-rpc form then submits as a native GET, fields in the URL.
//
//   - TestOldRuntimeClickFullLoads: the v0.86.0 bundle (pinned as a
//     testdata fixture, taken with `git show v0.86.0:…`) on a first
//     document in the old spelling clicks a same-chain link. The
//     navigate fetch carries no X-Gofastr-Markup, so uihost answers
//     the reload body; the click ends in a full document load with
//     today's runtime, and the destination's data-cui-rpc form posts
//     to its endpoint.
//   - TestNewRuntimeClickSoftSwaps: the control. Today's runtime sends
//     the header, gets the ordinary partial, swaps in place, and the
//     same form posts.

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// skewRig is a real uihost app (the NEW server). With old set, the
// first document for /home is rewritten to the v0.86.0 spelling and
// loads the pinned v0.86.0 bundle, the shape of a tab opened before
// the deploy. Every page request is counted by (path, partial?) and
// every hit on the form's endpoint by method and query.
type skewRig struct {
	srv *httptest.Server

	mu      sync.Mutex
	partial map[string]int
	full    map[string]int
	posts   int
	gets    []string
}

func (r *skewRig) counts(path string) (partial, full int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.partial[path], r.full[path]
}

func (r *skewRig) api() (posts int, gets []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.posts, append([]string(nil), r.gets...)
}

func newSkewRig(t *testing.T, old bool) *skewRig {
	t.Helper()
	oldJS, err := os.ReadFile("testdata/v0.86.0-runtime.js")
	if err != nil {
		t.Fatalf("v0.86.0 fixture: %v", err)
	}

	a := app.NewApp("skew-rig")
	site := app.NewLayout("site", app.LayoutSpec{}, func(ctx context.Context, l *app.LayoutTree) render.HTML {
		return render.Join(
			render.HTML(`<nav><a id="to-home" href="/home">Home</a> <a id="to-other" href="/other">Other</a></nav>`),
			l.Primary(),
		)
	})
	a.SetDefaultLayout(site)
	a.RegisterScreen(app.NewScreen("/home", app.NewStaticComponent(`<p id="screen-home">HOME</p>`)), nil)
	a.RegisterScreen(app.NewScreen("/other", app.NewStaticComponent(
		`<p id="screen-other">OTHER</p>`+
			`<form id="f" data-cui-rpc="/api/save" data-cui-rpc-method="POST">`+
			`<input name="secret" value="s3cret"><button type="submit" id="go">Save</button></form>`)), nil)

	ds := uihost.New(a)
	rt := router.New()
	ds.Mount(rt)

	r := &skewRig{partial: map[string]int{}, full: map[string]int{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/__old/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write(oldJS)
	})
	mux.HandleFunc("/api/save", func(w http.ResponseWriter, req *http.Request) {
		r.mu.Lock()
		if req.Method == http.MethodPost {
			r.posts++
		} else {
			r.gets = append(r.gets, req.URL.RawQuery)
		}
		r.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
		navigate := req.Header.Get("X-Gofastr-Navigate") == "1"
		r.mu.Lock()
		if navigate {
			r.partial[req.URL.Path]++
		} else if req.URL.Path == "/other" && req.URL.RawQuery != "" {
			// A native GET submit of the form lands here.
			r.gets = append(r.gets, req.URL.RawQuery)
		} else {
			r.full[req.URL.Path]++
		}
		r.mu.Unlock()
		if !old || navigate || req.URL.Path != "/home" {
			rt.ServeHTTP(w, req)
			return
		}
		// The tab opened before the deploy: today's /home document in
		// the v0.86.0 spelling, loading the v0.86.0 bundle.
		rec := httptest.NewRecorder()
		rt.ServeHTTP(rec, req)
		body, _ := io.ReadAll(rec.Body)
		doc := strings.ReplaceAll(string(body), "data-cui-", "data-fui-")
		doc = strings.ReplaceAll(doc, "/__gofastr/runtime.js", "/__old/runtime.js")
		for k, v := range rec.Header() {
			if k == "Content-Length" {
				continue
			}
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = io.WriteString(w, doc)
	})
	r.srv = httptest.NewServer(mux)
	t.Cleanup(r.srv.Close)
	return r
}

// runSkewClick loads /home, clicks through to /other, then submits the
// form. It reports whether the window survived the click (a soft swap)
// and whether today's runtime is the one running on /other.
func runSkewClick(t *testing.T, rig *skewRig) (soft, newRuntime bool) {
	t.Helper()
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var path string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(rig.srv.URL+"/home"),
		chromedp.WaitVisible(`#screen-home`, chromedp.ByID),
		chromedp.Poll(`document.readyState === 'complete' && !!window.__gofastr`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__beforeClick = 1`, nil),
		chromedp.Click(`#to-other`, chromedp.ByID),
		chromedp.WaitVisible(`#screen-other`, chromedp.ByID),
		chromedp.Poll(`document.readyState === 'complete' && location.pathname === '/other'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(500*time.Millisecond),
		chromedp.Evaluate(`location.pathname`, &path),
		chromedp.Evaluate(`window.__beforeClick === 1`, &soft),
		// data-cui-rpc forms are read by today's kernel only: the old
		// one scans for data-fui-rpc and never loads the rpc module.
		chromedp.Evaluate(`!!(document.querySelector('script[src^="/__gofastr/runtime.js"]') && !document.querySelector('script[src^="/__old/"]'))`, &newRuntime),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(800*time.Millisecond),
	); err != nil {
		t.Fatalf("run: %v", err)
	}
	if path != "/other" {
		t.Errorf("URL after the click = %s, want /other", path)
	}
	return soft, newRuntime
}

// TestOldRuntimeClickFullLoads: a v0.86.0 tab clicking a same-chain
// link after the upgrade ends in a full document load of /other with
// today's runtime, and the form there posts to its endpoint. Before
// the fix the old kernel soft-swapped today's markup in and the form
// submitted as a native GET with its fields in the URL.
func TestOldRuntimeClickFullLoads(t *testing.T) {
	rig := newSkewRig(t, true)
	soft, newRuntime := runSkewClick(t, rig)

	partial, full := rig.counts("/other")
	if partial != 1 {
		t.Errorf("partial requests for /other = %d, want exactly 1 (the click's fetch)", partial)
	}
	if soft || full < 1 {
		t.Errorf("the click soft-swapped (window kept=%v, full loads of /other=%d): an old kernel got today's markup", soft, full)
	}
	if !newRuntime {
		t.Errorf("/other is not running today's runtime after the click")
	}
	posts, gets := rig.api()
	if len(gets) > 0 {
		t.Errorf("the form submitted as a native GET with its fields in the URL: %q", gets)
	}
	if posts != 1 {
		t.Errorf("POSTs to /api/save = %d, want 1 (the data-cui-rpc submit)", posts)
	}
}

// TestNewRuntimeClickSoftSwaps: the control. Today's runtime names its
// markup version on the navigate fetch, gets the ordinary partial,
// swaps in place (no full load of /other) and the form posts.
func TestNewRuntimeClickSoftSwaps(t *testing.T) {
	rig := newSkewRig(t, false)
	soft, newRuntime := runSkewClick(t, rig)

	partial, full := rig.counts("/other")
	if partial != 1 || full != 0 || !soft {
		t.Errorf("today's runtime did not soft-swap: partial=%d full=%d window kept=%v", partial, full, soft)
	}
	if !newRuntime {
		t.Errorf("the control page is not running today's runtime")
	}
	posts, gets := rig.api()
	if len(gets) > 0 || posts != 1 {
		t.Errorf("form submit: posts=%d native GETs=%q, want 1 POST and no GET", posts, gets)
	}
}
