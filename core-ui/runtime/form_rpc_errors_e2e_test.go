package runtime

import (
	"fmt"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestDispatchRPC_FormFailureShowsFieldErrors: a form whose submission
// the server refuses with the validation envelope shows the field's
// error beside its control in framework/ui's own error markup (the
// FormField's fui-field__error, a bare checkbox's
// fui-choice-field__error after its label), marks the control invalid,
// and clears both on the next submission that succeeds. Before this the
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
			fmt.Fprint(w, `{"error":"validation failed","fields":{"estimate":["must be an integer"],"active":["must be true"]},"success":false}`)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>errors</title></head><body>
<form id="f" data-fui-rpc="/rpc/save" data-fui-rpc-method="POST">
  <div class="fui-field" data-fui-comp="ui-form-field">
    <label class="fui-field__label" for="f-estimate">Estimate</label>
    <input class="fui-input" id="f-estimate" name="estimate" value="">
  </div>
  <input type="hidden" name="active" value="false">
  <label class="fui-choice fui-choice--checkbox" data-fui-comp="ui-toggle"><input class="fui-choice__input" id="f-active" name="active" type="checkbox" value="true"><span class="fui-choice__text">Active</span></label>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var msg, invalid, describedBy string
	var boxMsg string
	var boxAfterLabel bool
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!!document.querySelector('.fui-field__error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Text(`.fui-field__error`, &msg, chromedp.ByQuery),
		chromedp.Text(`.fui-choice-field__error`, &boxMsg, chromedp.ByQuery),
		chromedp.Evaluate(`document.querySelector('[data-fui-comp="ui-toggle"]').nextElementSibling === document.querySelector('.fui-choice-field__error')`, &boxAfterLabel),
		chromedp.AttributeValue(`#f-estimate`, "aria-invalid", &invalid, nil, chromedp.ByID),
		chromedp.AttributeValue(`#f-estimate`, "aria-describedby", &describedBy, nil, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if msg != "must be an integer" || invalid != "true" || describedBy != "f-estimate-error" {
		t.Fatalf("after the refused submit: message=%q aria-invalid=%q describedby=%q", msg, invalid, describedBy)
	}
	if boxMsg != "must be true" || !boxAfterLabel {
		t.Fatalf("checkbox error: message=%q placed after its label=%v", boxMsg, boxAfterLabel)
	}

	// The next submission succeeds and the errors go away.
	var errCount int
	if err := chromedp.Run(ctx,
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.querySelectorAll('.fui-field__error, .fui-choice-field__error').length === 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('[aria-invalid="true"]').length`, &errCount),
	); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if errCount != 0 {
		t.Fatalf("aria-invalid controls after a successful submit = %d", errCount)
	}
}
