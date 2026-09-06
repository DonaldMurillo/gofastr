package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDispatchRPC_FormFailureShowsFieldErrors: a form whose submission
// the server refuses with the validation envelope shows the field's
// error in its own ui-form-field slot, marks the control invalid, and
// clears both on the next submission that succeeds. Before this the
// runtime swallowed the 400 and the user saw nothing move.
func TestDispatchRPC_FormFailureShowsFieldErrors(t *testing.T) {
	var hits atomic.Int32
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
	mux.HandleFunc("/rpc/save", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if hits.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"error":"validation failed","fields":{"estimate":["must be an integer"]},"success":false}`)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>errors</title></head><body>
<form id="f" data-fui-rpc="/rpc/save" data-fui-rpc-method="POST">
  <div class="ui-form-field" data-fui-comp="ui-form-field">
    <label for="f-estimate">Estimate</label>
    <input id="f-estimate" name="estimate" value="">
  </div>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := newSeedBrowserCtx(t)
	var msg, invalid, describedBy string
	var wrapErr bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('.ui-form-field__error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Text(`.ui-form-field__error`, &msg, chromedp.ByQuery),
		chromedp.AttributeValue(`#f-estimate`, "aria-invalid", &invalid, nil, chromedp.ByID),
		chromedp.AttributeValue(`#f-estimate`, "aria-describedby", &describedBy, nil, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('.ui-form-field').classList.contains('is-error')`, &wrapErr),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if msg != "must be an integer" || invalid != "true" || describedBy != "f-estimate-error" || !wrapErr {
		t.Fatalf("after the refused submit: message=%q aria-invalid=%q describedby=%q wrapper is-error=%v", msg, invalid, describedBy, wrapErr)
	}

	// The next submission succeeds and the errors go away.
	var errCount int
	if err := chromedp.Run(ctx,
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.querySelectorAll('.ui-form-field__error').length === 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('[aria-invalid="true"]').length`, &errCount),
	); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if errCount != 0 {
		t.Fatalf("aria-invalid controls after a successful submit = %d", errCount)
	}
}
