package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// A refused submit moves focus to the first refused control in the
// form's order, so a keyboard or screen-reader user lands on the field
// to fix (#704).
func TestRefusedSubmitFocusesFirstInvalidField(t *testing.T) {
	page := `<!doctype html><html><head><title>focus</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="name">Name</label><input id="name" name="name" value="ok"></div>
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <div data-hui-field=""><label for="city">City</label><input id="city" name="city" value=""></div>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	srv := formErrorsServer(t, page, func(int32) (int, string) {
		return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"city":["is required"],"email":["is required"]}}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var focused string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`!!document.getElementById('city-error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &focused),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if focused != "email" {
		t.Errorf("focus after the refused submit is on %q, want the first refused field %q", focused, "email")
	}
}

// A form that submits as the reader types keeps focus where the reader
// is typing when the server refuses another field.
func TestRefusedInputDispatchKeepsFocus(t *testing.T) {
	page := `<!doctype html><html><head><title>focus</title></head><body>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST" data-cui-rpc-trigger="input" data-cui-rpc-debounce-ms="10">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <div data-hui-field=""><label for="city">City</label><input id="city" name="city" value=""></div>
</form>
<script src="/__gofastr/runtime.js"></script>
</body></html>`
	srv := formErrorsServer(t, page, func(int32) (int, string) {
		return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"email":["is required"]}}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))

	var focused string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitVisible(`#city`, chromedp.ByID),
		chromedp.SendKeys(`#city`, "Lisbon", chromedp.ByID),
		chromedp.Poll(`!!document.getElementById('email-error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &focused),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	if focused != "city" {
		t.Errorf("focus after an as-you-type refusal is on %q, want it left on %q", focused, "city")
	}
}
