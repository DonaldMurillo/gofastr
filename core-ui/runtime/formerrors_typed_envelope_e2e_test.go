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
// of a handler.ValidationError (the envelope every typed handler,
// through HandlerAdapter, sends: {"error","code","reason","fields",
// "field_reasons","success"}), and /rpc/plain with a WriteError that
// names no field.
// The page records every toast the runtime asks for in window.__toasts.
func typedRefusalServer(t *testing.T, body string, names ...string) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	fieldName := "title"
	if len(names) > 0 {
		fieldName = names[0]
	}
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc/fields", func(w http.ResponseWriter, r *http.Request) {
		handler.WriteError(w, r, handler.ValidationError(map[string][]schema.Violation{
			fieldName: {{Reason: schema.ReasonRequired}},
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

func TestWireErrorsMatchNamesExactly(t *testing.T) {
	for _, name := range []string{"addressLine1", "address_line1"} {
		t.Run(name, func(t *testing.T) {
			body := fmt.Sprintf(`<form data-cui-rpc="/rpc/fields" data-cui-rpc-method="POST"><div data-hui-field><label for="author">Author</label><input id="author" name="%s"></div><button id="save" type="submit">Save</button></form>`, name)
			url := typedRefusalServer(t, body, "addressLine1")
			ctx := chromedptest.Context(t)
			var invalid string
			var toasts int
			if err := chromedp.Run(ctx, chromedp.Navigate(url), chromedp.Click(`#save`, chromedp.ByID),
				chromedp.Poll(`!!window.__gofastr.loadedModules.formerrors && (!!document.getElementById('author').getAttribute('aria-invalid') || window.__toasts.length>0)`, nil),
				chromedp.Evaluate(`document.getElementById('author').getAttribute('aria-invalid') || ''`, &invalid),
				chromedp.Evaluate(`window.__toasts.length`, &toasts),
			); err != nil {
				t.Fatal(err)
			}
			matched := name == "addressLine1"
			if (invalid == "true") != matched || (toasts == 0) != matched {
				t.Fatalf("field name %q: aria-invalid=%q toasts=%d; want an inline error only for an exact wire-key match", name, invalid, toasts)
			}
		})
	}
}

// A typed handler's refusal reaches the form. handler.WriteError used
// to nest the message and the field errors under "error"
// ({"error":{"message","fields"}}) while the formerrors module read only
// the flat CRUD envelope: the field got no aria-invalid and no error
// node, and the toast said "Request failed (400)" with the server's
// message lost. Both now write the one flat envelope (issue #652).
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
