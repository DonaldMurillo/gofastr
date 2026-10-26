package runtime

//gofastr:test-reads /framework/headless/leaveguard.js

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestFormIntercept_SubmitterPostOverrides(t *testing.T) {
	for _, encoding := range []string{"application/json", "multipart/form-data", "text/plain"} {
		t.Run(encoding, func(t *testing.T) {
			var recv atomic.Pointer[formRequest]
			base := startFormE2EServer(t, &recv)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(base+"/?spa=1"), chromedp.WaitVisible(`#go`, chromedp.ByID),
				chromedp.Evaluate(fmt.Sprintf(`(() => {
					const form = document.getElementById('f'); form.method = 'get'; form.action = '/wrong';
					const button = document.getElementById('go'); button.name = 'format'; button.value = 'selected';
					button.setAttribute('formmethod', 'post'); button.setAttribute('formaction', '/submit');
					button.setAttribute('formenctype', %q);
				})()`, encoding), nil),
				chromedp.Click(`#go`, chromedp.ByID), chromedp.WaitVisible(`#landed`, chromedp.ByID),
			); err != nil {
				t.Fatal(err)
			}
			got := recv.Load()
			if got == nil || got.method != "POST" || !strings.HasPrefix(got.contentType, encoding) {
				t.Fatalf("submitter overrides were lost: %+v", got)
			}
			if encoding == "multipart/form-data" {
				if got.formValues["format"] != "selected" {
					t.Fatalf("multipart omitted the selected submitter: %+v", got)
				}
			} else if !strings.Contains(got.jsonBody, "selected") {
				t.Fatalf("%s omitted the selected submitter: %+v", encoding, got)
			}
		})
	}
}

func TestFormIntercept_SubmitterTargetRemainsNative(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)
	var original bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/?spa=1"), chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			window.formDocumentProbe = 'kept';
			const frame = document.createElement('iframe'); frame.name = 'answer'; frame.id = 'answer'; document.body.append(frame);
			const button = document.getElementById('go'); button.name = 'chosen'; button.value = 'native'; button.setAttribute('formtarget', 'answer');
		})()`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!!document.getElementById('answer').contentDocument?.getElementById('landed')`, nil),
		chromedp.Evaluate(`window.formDocumentProbe === 'kept' && location.pathname === '/'`, &original),
	); err != nil {
		t.Fatal(err)
	}
	got := recv.Load()
	if !original || got == nil || got.formValues["chosen"] != "native" {
		t.Fatalf("formtarget did not use its native browsing context: preserved=%v request=%+v", original, got)
	}
}

func TestFormIntercept_GETBaseTargetRemainsNative(t *testing.T) {
	var query atomic.Value
	base := startPollServer(t, `<!doctype html><html><head><base target="answer"></head><body>
	<iframe id="answer" name="answer"></iframe>
	<form id="get-form" method="get" action="/result" data-cui-spa>
	<input name="q" value="files"><button id="go" name="view" value="grid">Search</button>
	</form><script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/result": func(w http.ResponseWriter, r *http.Request) {
			query.Store(r.Method + ":" + r.URL.Query().Get("q") + ":" + r.URL.Query().Get("view"))
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><p id="native-result">done</p></body></html>`)
		},
	})
	ctx := chromedptest.Context(t)
	var kept bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"), chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!!document.getElementById('answer')?.contentDocument?.getElementById('native-result')`, nil),
		chromedp.Evaluate(`location.pathname === '/' && !!document.getElementById('get-form') && !!document.getElementById('answer').contentDocument.getElementById('native-result')`, &kept),
	); err != nil {
		t.Fatal(err)
	}
	if !kept || query.Load() != "GET:files:grid" {
		t.Fatalf("base target did not retain the native frame submission: kept=%v, query=%v", kept, query.Load())
	}
}

func TestFormIntercept_GETMethodFieldDoesNotClobberMethod(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)
	var destination string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/?spa=1"), chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Evaluate(`(() => {
			const form = document.getElementById('f');
			form.setAttribute('method', 'get'); form.setAttribute('action', '/landing');
			form.innerHTML = '<input name="method" value="card"><button id="go">Search</button>';
			window.__gofastr.navigate = path => { window.methodFormDestination = path; };
		})()`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`typeof window.methodFormDestination === 'string'`, nil),
		chromedp.Evaluate(`window.methodFormDestination`, &destination),
	); err != nil {
		t.Fatal(err)
	}
	if destination != "/landing?method=card" {
		t.Fatalf("named method control broke GET client navigation: %s", destination)
	}
}

func TestFormIntercept_GETMissingModulePreservesSubmitter(t *testing.T) {
	var query atomic.Value
	base := startPollServer(t, `<!doctype html><html><body>
	<form method="post" action="/wrong" enctype="application/json" data-cui-spa>
	<input name="q" value="notes"><button id="go" name="submit" value="files" formmethod="get" formaction="/result" formenctype="application/x-www-form-urlencoded">Search</button>
	</form><script src="/__gofastr/runtime.js"></script></body></html>`, map[string]http.HandlerFunc{
		"/__gofastr/runtime/rpc.js": func(w http.ResponseWriter, r *http.Request) { http.Error(w, "missing", http.StatusServiceUnavailable) },
		"/result": func(w http.ResponseWriter, r *http.Request) {
			query.Store(r.URL.Query().Get("q") + ":" + r.URL.Query().Get("submit"))
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<html><body><div id="done">done</div></body></html>`)
		},
	})
	ctx := chromedptest.Context(t)
	if err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), chromedp.WaitVisible(`#go`, chromedp.ByID), chromedp.Click(`#go`, chromedp.ByID), chromedp.WaitVisible(`#done`, chromedp.ByID)); err != nil {
		t.Fatal(err)
	}
	if got := query.Load(); got != "notes:files" {
		t.Fatalf("module failure lost the submitter or overrides: %v", got)
	}
}

// Exercise the real headless guard and themed asynchronous Cancel/Discard
// path, rather than stubbing the guard API whose invocation is under test.
func TestFormIntercept_GETRespectsLeaveGuard(t *testing.T) {
	guard, err := os.ReadFile("../../framework/headless/leaveguard.js")
	if err != nil {
		t.Fatal(err)
	}
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, js)
	})
	mux.HandleFunc("/guard.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write(guard)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><script type="application/json" id="gofastr-routes">[{"path":"/"},{"path":"/away"}]</script></head><body><main role="main" tabindex="-1">
		<form id="dirty" data-hui-leave-guard data-hui-leave-guard-message="Discard edits?"><input id="edit" name="name"></form>
		<form id="search" method="get" action="/away#result" data-cui-spa><input name="q" value="files"><button id="go">Search</button></form>
		</main>`+confirmTemplate+`<script src="/__gofastr/runtime.js"></script><script src="/guard.js"></script></body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t)
	var afterCancel, afterDiscard bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"), chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr._leaveGuard?.ok === 'function'`, nil),
		chromedp.SendKeys(`#edit`, "unsaved", chromedp.ByID),
		// Even when an intercept resolver would claim this as a harmless
		// overlay, NS.navigate replaces the page and must guard its edits.
		chromedp.Evaluate(`window.__gofastr._interceptScope = () => null; window.confirm = () => {throw new Error('themed guard required')}`, nil),
		chromedp.Click(`#go`, chromedp.ByID), chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil, chromedp.WithPollingTimeout(4*time.Second)),
		chromedp.Click(`body > dialog [data-cui-confirm-part="cancel"]`, chromedp.ByQuery),
		chromedp.Sleep(150*time.Millisecond),
		chromedp.Evaluate(`location.pathname === '/' && document.getElementById('edit').value === 'unsaved' && document.getElementById('dirty').hasAttribute('data-hui-dirty')`, &afterCancel),
		chromedp.Click(`#go`, chromedp.ByID), chromedp.Poll(`!!document.querySelector('body > dialog[open]')`, nil, chromedp.WithPollingTimeout(4*time.Second)),
		chromedp.Click(`body > dialog [data-cui-confirm-part="accept-danger"]`, chromedp.ByQuery),
		chromedp.Poll(`location.pathname === '/away' && location.search === '?q=files' && location.hash === '#result'`, nil),
		chromedp.Evaluate(`!document.getElementById('dirty').hasAttribute('data-hui-dirty')`, &afterDiscard),
	); err != nil {
		t.Fatal(err)
	}
	if !afterCancel || !afterDiscard {
		t.Fatalf("leave guard cancel/discard failed: cancel=%v discard=%v", afterCancel, afterDiscard)
	}
}
