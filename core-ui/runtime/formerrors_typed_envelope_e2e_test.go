package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// typedRefusalServer answers /rpc/fields with the real handler.WriteError
// of a handler.ValidationError (the shape every typed handler, through
// HandlerAdapter, sends: {"success":false,"error":{"code","message",
// "fields"}}), and /rpc/plain with a WriteError that names no field.
// The page records every toast the runtime asks for in window.__toasts.
func typedRefusalServer(t *testing.T, body string) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc/fields", func(w http.ResponseWriter, r *http.Request) {
		handler.WriteError(w, r, handler.ValidationError(map[string][]schema.Violation{
			"title": {{Reason: schema.ReasonRequired}},
		}))
	})
	mux.HandleFunc("/rpc/plain", func(w http.ResponseWriter, r *http.Request) {
		handler.WriteError(w, r, handler.ValidationError(nil))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>typed</title></head><body>%s
<span id="ready">ready</span>
<script src="/__gofastr/runtime.js"></script>
<script>window.__toasts = []; window.__gofastr._toastOrFallback = (c) => { window.__toasts.push(c); return Promise.resolve(null); };</script>
</body></html>`, body)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL + "/"
}

// A typed handler's refusal reaches the form. handler.WriteError nests
// the message and the field errors under "error" ({"error":{"message",
// "fields"}}), while the formerrors module read only the flat CRUD
// envelope ({"error": "...", "fields"}): the field got no aria-invalid
// and no error node, and the toast said "Request failed (400)" with the
// server's message lost.
func TestFormErrorsReadTypedHandlerEnvelope(t *testing.T) {
	url := typedRefusalServer(t, `
<form id="f" data-cui-rpc="/rpc/fields" data-cui-rpc-method="POST">
  <div data-hui-field="">
    <label for="f-title">Title</label>
    <input id="f-title" name="title" value="">
  </div>
  <button type="submit" id="go">Save</button>
</form>
<form id="g" data-cui-rpc="/rpc/plain" data-cui-rpc-method="POST">
  <button type="submit" id="go2">Save</button>
</form>`)

	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var invalid, msg string
	var toastTitle string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr.loadedModules.formerrors === true && (document.getElementById('f-title').getAttribute('aria-invalid') === 'true' || window.__toasts.length > 0)`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('f-title').getAttribute('aria-invalid') || ''`, &invalid),
		chromedp.Evaluate(`(document.getElementById('f-title-error') || {}).textContent || ''`, &msg),
		chromedp.Click(`#go2`, chromedp.ByID),
		chromedp.Poll(`window.__toasts.length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__toasts[window.__toasts.length - 1].title`, &toastTitle),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if invalid != "true" || msg == "" {
		t.Fatalf("typed handler refusal: aria-invalid=%q error node=%q, want the field marked and its message shown", invalid, msg)
	}
	if toastTitle != "validation failed" {
		t.Fatalf("a refusal naming no field toasts %q, want the server's message %q", toastTitle, "validation failed")
	}
}

// An opted-in RPC button (data-cui-rpc-error-toast) carries the
// server's message in the toast body for the typed envelope too.
func TestRPCErrorToastReadsTypedHandlerEnvelope(t *testing.T) {
	url := typedRefusalServer(t, `<button id="b" data-cui-rpc="/rpc/plain" data-cui-rpc-error-toast="Could not save">Go</button>`)
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var body string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(url),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Click(`#b`, chromedp.ByID),
		chromedp.Poll(`window.__toasts.length > 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`window.__toasts[0].body`, &body),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if body != "validation failed" {
		t.Fatalf("error toast body = %q, want the server's message %q", body, "validation failed")
	}
}
