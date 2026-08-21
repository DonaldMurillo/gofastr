package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// gadgetServer serves a page with the real runtime plus two instrumented
// endpoints. Counting requests is the point: whether a gadget fired is a
// question about what left the browser, not about what the DOM looks
// like afterwards.
type gadgetServer struct {
	Srv    *httptest.Server
	EvilJS atomic.Int32
	// EvilFetch counts only RUNTIME-originated requests to /steal. A
	// native browser form submit to a cross-origin action is ordinary
	// HTML and not what any of this is about; what matters is whether
	// the runtime fetched the URL itself, because that is the request
	// that carries the session cookie context, the CSRF token, and a
	// response the runtime will put in the DOM.
	EvilFetch atomic.Int32
	EvilAny   atomic.Int32
}

// startGadgetServer serves body inside a page that loads the runtime.
// widgets is the JSON the runtime's widget-catalog fetch returns ("[]"
// for none).
func startGadgetServer(t *testing.T, widgets, body string) *gadgetServer {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	g := &gadgetServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		src, ok := Module(name)
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(src))
	})
	mux.HandleFunc("/__gofastr/widgets", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(widgets))
	})
	// Stands in for a same-origin JS route an attacker can reach: an
	// upload store, a generated SDK .js, a plugin asset. Under strict
	// CSP a same-origin script still executes, which is why CSP is not
	// an answer to the data-behavior sink.
	mux.HandleFunc("/evil.js", func(w http.ResponseWriter, r *http.Request) {
		g.EvilJS.Add(1)
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(`window.__pwned = true;`))
	})
	mux.HandleFunc("/chrome/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<div data-fui-widget="dl">chrome</div>`))
	})
	mux.HandleFunc("/steal", func(w http.ResponseWriter, r *http.Request) {
		g.EvilAny.Add(1)
		// Sec-Fetch-Mode is "cors" for fetch()/XHR and "navigate" for a
		// document-level form submit. The runtime also stamps the CSRF
		// header, which a native submit never does.
		if r.Header.Get("Sec-Fetch-Mode") == "cors" || r.Header.Get("X-CSRF-Token") != "" ||
			strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			g.EvilFetch.Add(1)
		}
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "*")
		w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html><head><title>gadget</title></head><body>
%s
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
</body></html>`, body)
	})
	g.Srv = httptest.NewServer(mux)
	t.Cleanup(g.Srv.Close)
	return g
}

// TestBehaviorAttrRejectsForeignSrc pins that `data-behavior` only loads
// the one script shape the framework emits.
//
// Attack: hydrate() read data-behavior off any [data-widget] /
// [data-component] element and appended <script src=that> with no
// allow-list and no origin check, and setupMutationObserver rewired it
// onto elements inserted by later island/SPA swaps. Attribute injection,
// or an untrusted IR, before core-ui/noderender's allow-list, turned
// a hover or focus into arbitrary script execution. Cross-origin was
// stopped only by CSP; a SAME-origin JS route executed under strict CSP.
//
// The legitimate shape is the emitter's own: /__gofastr/widget/<id>.js
// (core-ui/component/component.go).
func TestBehaviorAttrRejectsForeignSrc(t *testing.T) {
	g := startGadgetServer(t, `[]`, `<div id="host"></div>`)

	ctx := newSeedBrowserCtx(t)
	var pwned bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// Insert the element AFTER boot: hydration listeners are wired by
		// setupMutationObserver, so this is the reachable shape, markup
		// arriving from an island swap, an RPC innerHTML replacement, or
		// an SPA page merge.
		chromedp.Evaluate(`document.getElementById('host').innerHTML =
			'<div id="w" data-widget="w1" data-behavior="/evil.js" tabindex="0">widget</div>'; true`, nil),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('w').dispatchEvent(new Event('mouseenter')); true`, nil),
		chromedp.Sleep(400*time.Millisecond),
		chromedp.Evaluate(`window.__pwned === true`, &pwned),
	); err != nil {
		t.Fatal(err)
	}
	if pwned || g.EvilJS.Load() > 0 {
		t.Errorf("SECURITY: [xss] data-behavior loaded a foreign script (requests=%d, executed=%v). Attack: attribute injection turns focus into script execution",
			g.EvilJS.Load(), pwned)
	}
}

// TestFormActionRejectsProtocolRelative pins that the SPA form-submit
// interceptor's cross-origin test cannot be defeated by a
// protocol-relative action.
//
// Attack: the guard was `action.match(/^https?:\/\//)`, bail out when
// the action is absolute. `//evil.example/steal` starts with neither
// `http` nor `https`, so it fell through to the fetch path and the form
// body was posted cross-origin (stopped only by CSP connect-src).
func TestFormActionRejectsProtocolRelative(t *testing.T) {
	g := startGadgetServer(t, `[]`, `
<form id="f" enctype="application/json" method="post">
  <input name="secret" value="s3cret">
  <button id="go" type="submit">go</button>
</form>`)

	ctx := newSeedBrowserCtx(t)
	// The action points back at this server so a hit proves the guard
	// let it through, rather than proving an unreachable host failed.
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// localhost and 127.0.0.1 are different ORIGINS to the browser
		// but the same server here, so a hit is unambiguous evidence
		// that the guard let a cross-origin action through.
		chromedp.Evaluate(`document.getElementById('f').setAttribute('action','//localhost:'+location.port+'/steal'); true`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if g.EvilFetch.Load() > 0 {
		t.Errorf(`SECURITY: [exfil] a protocol-relative form action was fetched by the SPA interceptor (%d hits). Attack: //host/path defeats a /^https?:\/\// origin check`,
			g.EvilFetch.Load())
	}
}

// TestRuntimeFetchRefusesForeignOrigin pins that a DOM-attribute-supplied
// RPC URL cannot point the runtime, and the CSRF token it attaches, at
// another origin.
//
// Attack: dispatchRPC takes its URL from data-fui-rpc with no origin
// check and attaches X-CSRF-Token to whatever host the attribute names;
// the response often lands in innerHTML. The runtime already treats this
// class as real for data-kiln-tool (gated behind _kilnOK); the same
// reasoning was never applied to the other fetch sites.
func TestRuntimeFetchRefusesForeignOrigin(t *testing.T) {
	g := startGadgetServer(t, `[]`, `
<button id="rpc" data-fui-rpc="/placeholder" data-fui-rpc-method="POST">go</button>`)

	ctx := newSeedBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		// localhost and 127.0.0.1 are different origins to the browser
		// but the same server here, so a hit is unambiguous evidence.
		chromedp.Evaluate(`document.getElementById('rpc').setAttribute('data-fui-rpc','http://localhost:'+location.port+'/steal'); true`, nil),
		chromedp.Click(`#rpc`, chromedp.ByID),
		chromedp.Sleep(400*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if g.EvilFetch.Load() > 0 {
		t.Errorf("SECURITY: [csrf] data-fui-rpc fetched a foreign origin (%d hits) and forwarded the CSRF token with it", g.EvilFetch.Load())
	}
}

// TestSetSignalRejectsProtoKey pins that a DOM-attribute-controlled
// signal name cannot re-parent the signal store.
//
// Attack: isReservedSignalKey guarded the three seed-merge loops but not
// setSignal itself, where attribute-controlled keys enter via
// data-fui-signal-set / -inc / -toggle and via fetched-JSON keys in
// poll.js and widgets.js. `data-fui-signal-set="__proto__:POLLUTED"`
// makes ({}).value === "POLLUTED"; because getSignal is `s ? s.value :
// undefined`, every unset signal then reads back the attacker's string.
// That is pure data corruption, so it is the one gadget in this family
// CSP does not stop.
func TestSetSignalRejectsProtoKey(t *testing.T) {
	g := startGadgetServer(t, `[]`, `
<button id="pollute" data-fui-signal-set="__proto__:POLLUTED">pollute</button>`)

	ctx := newSeedBrowserCtx(t)
	var polluted, leaked string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#pollute`, chromedp.ByID),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`String(({}).value)`, &polluted),
		chromedp.Evaluate(`String(window.__gofastr.getSignal('never_set_anywhere'))`, &leaked),
	); err != nil {
		t.Fatal(err)
	}
	if polluted != "undefined" {
		t.Errorf("SECURITY: [prototype-pollution] setSignal wrote through __proto__ — ({}).value = %q", polluted)
	}
	if leaked != "undefined" {
		t.Errorf("SECURITY: [prototype-pollution] an unset signal read back %q — the store's prototype was replaced", leaked)
	}
}

// TestDeepLinkParamNeverReachesInnerHTML pins that a value seeded from
// location.search cannot be rendered as HTML.
//
// Attack: setSignal's html mode does `innerHTML = value` with only a
// type check (attr mode DOES guard), and widgets.js seeds signals
// straight from location.search for every .DeepLinkParam("x") widget. A
// page carrying an html-mode binding of `x` therefore reflects
// `?x=<payload>` into the DOM.
func TestDeepLinkParamNeverReachesInnerHTML(t *testing.T) {
	// hidden:true keeps the widget out of the boot auto-mount, so
	// _syncDeepLinks is what opens it, that is the path that seeds
	// declared deep-link params straight from location.search.
	catalog := `[{"hidden":true,"cfg":{"name":"dl","deepLinkParams":["x"],` +
		`"deepLinkKey":"pane","deepLinkValue":"dl","chromePath":"/chrome/dl",` +
		`"stylePath":"/chrome/dl.css"}}]`
	g := startGadgetServer(t, catalog, `
<div id="sink" data-fui-signal="x" data-fui-signal-mode="html">initial</div>`)

	ctx := newSeedBrowserCtx(t)
	var sink string
	var pwned bool
	if err := chromedp.Run(ctx,
		// pane=dl is what makes _syncDeepLinks open the widget, which is
		// the code path that seeds declared deep-link params from the URL.
		chromedp.Navigate(g.Srv.URL+`/?pane=dl&x=%3Cimg+src%3Dx+onerror%3D%22window.__pwned%3Dtrue%22%3E`),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Sleep(600*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('sink').innerHTML`, &sink),
		chromedp.Evaluate(`window.__pwned === true`, &pwned),
	); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sink, "<img") || pwned {
		t.Errorf("SECURITY: [xss] a query-string-seeded signal was written as HTML (executed=%v): %s", pwned, sink)
	}
	// The value must still reach the node, escaped. Dropping it
	// entirely would be a different bug.
	if !strings.Contains(sink, "onerror") {
		t.Errorf("deep-link value did not reach the node at all: %q", sink)
	}
}
