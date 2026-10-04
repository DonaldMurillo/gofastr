package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// ssrErroredFormPage is the shape headless.Field renders for a form
// the server refused on a full page: each errored control carries
// aria-invalid and points at a filled error paragraph whose hook value
// is empty (the server's, not the module's).
const ssrErroredFormPage = `<!doctype html><html><head><title>ssr errors</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field="">
    <label for="a">A</label>
    <input id="a" name="a" value="" aria-invalid="true" aria-describedby="a-error">
    <p id="a-error" role="alert" data-hui-field-error="">A was wrong</p>
  </div>
  <div data-hui-field="">
    <label for="b">B</label>
    <input id="b" name="b" value="" aria-invalid="true" aria-describedby="b-error">
    <p id="b-error" role="alert" data-hui-field-error="">B was wrong</p>
  </div>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`

const ssrErrorState = `JSON.stringify({
	a: document.getElementById('a-error').textContent,
	b: document.getElementById('b-error').textContent,
	aInvalid: document.getElementById('a').getAttribute('aria-invalid') || '',
	bInvalid: document.getElementById('b').getAttribute('aria-invalid') || '',
})`

// TestSSRErrorClearedOnJSRetry: a page served with errors on A and B
// retries through data-cui-rpc and the server now refuses only B. A's
// server-rendered message goes with its aria-invalid; B shows the new
// message. Before the fix clear() emptied only nodes the module had
// filled, so A's stale message stayed on screen.
func TestSSRErrorClearedOnJSRetry(t *testing.T) {
	srv := formErrorsServer(t, ssrErroredFormPage, func(int32) (int, string) {
		return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"b":["B is still wrong"]}}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('b-error').textContent === 'B is still wrong'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(ssrErrorState, &state),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	want := `{"a":"","b":"B is still wrong","aInvalid":"","bInvalid":"true"}`
	if state != want {
		t.Errorf("after the retry refused only B:\n got %s\nwant %s", state, want)
	}
}

// TestSSRErrorClearedOnJSSuccess: the same page whose first retry
// succeeds. The formerrors module has not loaded (nothing failed yet
// in this document), and the server's messages still have to go.
func TestSSRErrorClearedOnJSSuccess(t *testing.T) {
	srv := formErrorsServer(t, ssrErroredFormPage, func(int32) (int, string) {
		return http.StatusOK, `{"ok":true}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var state string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('a-error').textContent === '' && document.getElementById('b-error').textContent === ''`, nil, chromedp.WithPollingTimeout(5*time.Second)),
		chromedp.Evaluate(ssrErrorState, &state),
	); err != nil {
		t.Fatalf("chromedp: %v (the server's messages stayed after a successful retry)", err)
	}
	if want := `{"a":"","b":"","aInvalid":"","bInvalid":""}`; state != want {
		t.Errorf("after a successful retry:\n got %s\nwant %s", state, want)
	}
}
