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
// the server refuses with the validation envelope shows each field's
// error beside its control through the headless hooks and nothing
// else: a rendered error node ([data-hui-field-error], here the
// reserved empty one) is filled in place and marked "filled"; a field
// without one gets a live paragraph (value "live") as its last child;
// a bare choice gets it right after the label. Each control is marked
// invalid and described by its node, and the next submission that
// succeeds clears all of it: live nodes go, the reserved node empties
// back to its reserved state. The module names no kit class, so the
// live nodes carry none. Before this the runtime swallowed the 400
// and the user saw nothing move.
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
			fmt.Fprint(w, `{"error":"validation failed","fields":{"estimate":["must be an integer"],"title":["required"],"active":["must be true"]},"success":false}`)
			return
		}
		fmt.Fprint(w, `{"ok":true}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><head><title>errors</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div class="fui-field" data-hui-field="" data-cui-comp="ui-form-field">
    <label class="fui-field__label" for="f-estimate">Estimate</label>
    <input class="fui-input" id="f-estimate" name="estimate" value="" aria-describedby="f-estimate-error">
    <p class="fui-field__error" data-hui-field-error="" id="f-estimate-error" role="alert"></p>
  </div>
  <div class="fui-field" data-hui-field="" data-cui-comp="ui-form-field">
    <label class="fui-field__label" for="f-title">Title</label>
    <input class="fui-input" id="f-title" name="title" value="">
  </div>
  <input type="hidden" name="active" value="false">
  <label class="fui-choice fui-choice--checkbox" data-hui-choice="" data-cui-comp="ui-toggle"><input class="fui-choice__input" id="f-active" name="active" type="checkbox" value="true"><span class="fui-choice__text">Active</span></label>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t, chromedptest.Timeout(90*time.Second))
	var reservedMsg, reservedState, invalid, describedBy string
	var reservedNodes int
	var liveMsg, liveID, liveDescribedBy, boxMsg string
	var boxAfterLabel bool
	var classedLive int
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('f-estimate-error').getAttribute('data-hui-field-error') === 'filled'`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Text(`#f-estimate-error`, &reservedMsg, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('f-estimate-error').getAttribute('data-hui-field-error')`, &reservedState),
		chromedp.Evaluate(`document.getElementById('f-estimate').closest('[data-hui-field]').querySelectorAll('[data-hui-field-error]').length`, &reservedNodes),
		chromedp.AttributeValue(`#f-estimate`, "aria-invalid", &invalid, nil, chromedp.ByID),
		chromedp.AttributeValue(`#f-estimate`, "aria-describedby", &describedBy, nil, chromedp.ByID),
		chromedp.Evaluate(`document.getElementById('f-title').closest('[data-hui-field]').lastElementChild.textContent`, &liveMsg),
		chromedp.Evaluate(`document.getElementById('f-title').closest('[data-hui-field]').lastElementChild.id`, &liveID),
		chromedp.AttributeValue(`#f-title`, "aria-describedby", &liveDescribedBy, nil, chromedp.ByID),
		chromedp.Evaluate(`document.querySelector('[data-hui-choice]').nextElementSibling.textContent`, &boxMsg),
		chromedp.Evaluate(`document.querySelector('[data-hui-choice]').nextElementSibling.matches('p[role="alert"][data-hui-field-error="live"]')`, &boxAfterLabel),
		chromedp.Evaluate(`document.querySelectorAll('[data-hui-field-error="live"][class]').length`, &classedLive),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if reservedMsg != "must be an integer" || reservedState != "filled" || reservedNodes != 1 || invalid != "true" || describedBy != "f-estimate-error" {
		t.Fatalf("reserved node after the refused submit: message=%q state=%q nodes=%d aria-invalid=%q describedby=%q", reservedMsg, reservedState, reservedNodes, invalid, describedBy)
	}
	if liveMsg != "required" || liveID != "f-title-error" || liveDescribedBy != "f-title-error" {
		t.Fatalf("live node: message=%q id=%q describedby=%q", liveMsg, liveID, liveDescribedBy)
	}
	if boxMsg != "must be true" || !boxAfterLabel {
		t.Fatalf("checkbox error: message=%q live alert after its label=%v", boxMsg, boxAfterLabel)
	}
	if classedLive != 0 {
		t.Fatalf("%d live error nodes carry a class: the module names no kit class", classedLive)
	}

	// The next submission succeeds and the errors go away: live nodes
	// are removed, the reserved node is reserved again.
	var errCount int
	var reservedText string
	if err := chromedp.Run(ctx,
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.querySelectorAll('[data-hui-field-error="live"], [data-hui-field-error="filled"]').length === 0`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.querySelectorAll('[aria-invalid="true"]').length`, &errCount),
		chromedp.Evaluate(`document.getElementById('f-estimate-error').textContent + '|' + document.getElementById('f-estimate-error').getAttribute('data-hui-field-error')`, &reservedText),
	); err != nil {
		t.Fatalf("second submit: %v", err)
	}
	if errCount != 0 {
		t.Fatalf("aria-invalid controls after a successful submit = %d", errCount)
	}
	if reservedText != "|" {
		t.Fatalf("reserved node after a successful submit = %q, want empty and reserved", reservedText)
	}
}
