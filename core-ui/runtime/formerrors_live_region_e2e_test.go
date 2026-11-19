package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// A refused submit is read once, from the document's one polite
// region, as one "Label: message" sentence per refused field; no error
// node is an alert of its own, and a successful retry empties the
// region (#704).
func TestRefusalIsReadFromOnePoliteRegion(t *testing.T) {
	page := `<!doctype html><html><head><title>live</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <div data-hui-field=""><label for="city">City</label><input id="city" name="city" value=""><p id="city-error" data-hui-field-error=""></p></div>
  <fieldset data-hui-field=""><legend>Plan</legend>
    <label data-hui-choice=""><input type="radio" id="p-free" name="plan" value="free"> Free</label>
    <label data-hui-choice=""><input type="radio" id="p-pro" name="plan" value="pro"> Pro</label>
  </fieldset>
  <button type="submit" id="go">Save</button>
</form><script src="/__gofastr/runtime.js"></script></body></html>`
	srv := formErrorsServer(t, page, func(n int32) (int, string) {
		if n == 1 {
			return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"email":["is required"],"city":["is required","too short"],"plan":["pick one"]}}`
		}
		return http.StatusOK, `{}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var got struct {
		Text   string `json:"text"`
		Role   string `json:"role"`
		Live   string `json:"live"`
		Alerts int    `json:"alerts"`
		Count  int    `json:"count"`
	}
	var after string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`(document.getElementById('cui-form-errors')?.textContent || '') !== ''`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`(() => { const r = document.getElementById('cui-form-errors');
			return { text: r.textContent, role: r.getAttribute('role'), live: r.getAttribute('aria-live'),
				alerts: document.querySelectorAll('[role="alert"]').length,
				count: document.querySelectorAll('#cui-form-errors').length }; })()`, &got),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!document.getElementById('email').hasAttribute('aria-invalid')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`document.getElementById('cui-form-errors').textContent`, &after),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if want := "Email: is required. City: is required, too short. Plan: pick one"; got.Text != want {
		t.Errorf("the region reads %q, want %q", got.Text, want)
	}
	if got.Role != "status" || got.Live != "polite" || got.Count != 1 {
		t.Errorf("want one polite status region, got role=%q aria-live=%q count=%d", got.Role, got.Live, got.Count)
	}
	if got.Alerts != 0 {
		t.Errorf("%d error nodes are alerts of their own; a refusal is read from the one region", got.Alerts)
	}
	if after != "" {
		t.Errorf("a successful retry left the region reading %q", after)
	}
}
