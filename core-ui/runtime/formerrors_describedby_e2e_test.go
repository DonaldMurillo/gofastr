package runtime

// The formerrors module's aria-describedby and server-rendered error
// handling: a refused data-cui-rpc submission adds the error node to a
// control's description without dropping what was there (a hint), a
// retry removes only what it added, and an error message the server
// rendered with the page is the module's to clear once the form
// retries through JavaScript.

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// formErrorsServer serves page at "/" and answers POST /rpc/save with
// the response answer returns for the nth request (1-based).
func formErrorsServer(t *testing.T, page string, answer func(n int32) (int, string)) *httptest.Server {
	t.Helper()
	var hits atomic.Int32
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc/save", func(w http.ResponseWriter, _ *http.Request) {
		status, body := answer(hits.Add(1))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, page)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestFormErrorKeepsHintDescribedBy: a control described by its hint
// keeps the hint id when the module adds its error node, and the
// successful retry leaves the hint alone: no dangling reference to the
// removed error node. Before the fix report() replaced the list with
// the error id and clear() never restored it.
func TestFormErrorKeepsHintDescribedBy(t *testing.T) {
	page := `<!doctype html><html><head><title>hint</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field="">
    <label for="email">Email</label>
    <input id="email" name="email" value="" aria-describedby="email-hint">
    <p id="email-hint">We never share it.</p>
  </div>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	srv := formErrorsServer(t, page, func(n int32) (int, string) {
		if n == 1 {
			return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"email":["is not an address"]}}`
		}
		return http.StatusOK, `{"ok":true}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var afterReport, afterClear string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!!document.getElementById('email-error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.getElementById('email').getAttribute('aria-describedby') || ''`, &afterReport),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!document.getElementById('email-error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(200*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('email').getAttribute('aria-describedby') || ''`, &afterClear),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if afterReport != "email-hint email-error" {
		t.Errorf("aria-describedby after the refused submit = %q, want %q (hint kept, error added)", afterReport, "email-hint email-error")
	}
	if afterClear != "email-hint" {
		t.Errorf("aria-describedby after the successful retry = %q, want %q", afterClear, "email-hint")
	}
}
