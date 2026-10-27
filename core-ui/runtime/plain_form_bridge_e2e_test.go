package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/middleware"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// plainFormPage serves runtime.js, its modules, and one page whose body is
// form. submit handles the form's POST.
func plainFormPage(t *testing.T, head, form string, submit http.Handler, wrap func(http.Handler) http.Handler) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if wrap == nil {
		wrap = func(h http.Handler) http.Handler { return h }
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.Handle("/submit", wrap(submit))
	mux.Handle("/", wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := middleware.MaskCSRFToken(middleware.TokenFromContext(r.Context()))
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>plain form</title>%s</head><body>%s
<script src="/__gofastr/runtime.js"></script></body></html>`,
			strings.ReplaceAll(head, "{{tok}}", tok), strings.ReplaceAll(form, "{{tok}}", tok))
	})))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

// submitPlainForm clicks #go and waits for the bridge's formresult.
func submitPlainForm(t *testing.T, url string, after ...chromedp.Action) {
	t.Helper()
	ctx := chromedptest.Context(t)
	actions := []chromedp.Action{
		chromedp.Navigate(url + "/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr.loadModule === 'function'`, nil),
		chromedp.Evaluate(`window.__seen = []; document.getElementById('f').addEventListener('gofastr:formresult', e => window.__seen.push(e.detail.ok));`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`window.__seen.length > 0`, nil, chromedp.WithPollingTimeout(5*time.Second)),
	}
	if err := chromedp.Run(ctx, append(actions, after...)...); err != nil {
		t.Fatal(err)
	}
}

// A JSON body carries no form field middleware.CSRF can read, so the
// bridge sends the page's token as the X-CSRF-Token header, the way
// data-cui-rpc does.
func TestJSONFormSendsCSRFHeader(t *testing.T) {
	var status atomic.Int64
	csrf := middleware.CSRF(middleware.CSRFConfig{CookieName: "auth_csrf"})
	url := plainFormPage(t, `<meta name="csrf-token" content="{{tok}}">`,
		`<form id="f" action="/submit" method="POST" enctype="application/json">
  <input type="text" name="email" value="a@b.com">
  <button id="go" type="submit">Submit</button>
</form>`,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }),
		func(next http.Handler) http.Handler {
			guarded := csrf(next)
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				guarded.ServeHTTP(rec, r)
				if r.URL.Path == "/submit" {
					status.Store(int64(rec.Code))
				}
				for k, v := range rec.Header() {
					w.Header()[k] = v
				}
				w.WriteHeader(rec.Code)
				w.Write(rec.Body.Bytes())
			})
		})
	submitPlainForm(t, url)
	if got := status.Load(); got != http.StatusNoContent {
		t.Fatalf("JSON form behind middleware.CSRF answered %d, want 204", got)
	}
}

// A refused data-cui-spa submit shows the server's field errors in place,
// like a refused data-cui-rpc form.
func TestSPAFormShowsFieldErrors(t *testing.T) {
	url := plainFormPage(t, "",
		`<form id="f" action="/submit" method="POST" data-cui-spa>
  <div data-hui-field><input id="email" type="text" name="email" value="a@b.com"></div>
  <button id="go" type="submit">Submit</button>
</form>`,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, `{"error":"Validation failed","fields":{"email":["already taken"]}}`)
		}), nil)
	var msg string
	submitPlainForm(t, url,
		chromedp.Poll(`document.getElementById('email').getAttribute('aria-invalid') === 'true'`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Text(`[data-hui-field-error]`, &msg, chromedp.ByQuery),
	)
	if msg != "already taken" {
		t.Fatalf("field error = %q, want %q", msg, "already taken")
	}
}

// A data-cui-spa submit answered 2xx with no redirect still delivers the
// server's toast.
func TestSPAFormDeliversToastHeader(t *testing.T) {
	url := plainFormPage(t, "",
		`<form id="f" action="/submit" method="POST" data-cui-spa>
  <input type="text" name="email" value="a@b.com">
  <button id="go" type="submit">Submit</button>
</form>`,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Gofastr-Toast", `[{"variant":"success","title":"Saved"}]`)
			w.WriteHeader(http.StatusNoContent)
		}), nil)
	var title string
	submitPlainForm(t, url,
		chromedp.Poll(`!!document.querySelector('[data-cui-toast-fallback] strong')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Text(`[data-cui-toast-fallback] strong`, &title, chromedp.ByQuery),
	)
	if title != "Saved" {
		t.Fatalf("toast title = %q, want Saved", title)
	}
}
