package runtime

import (
	"net/http"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
)

// validityPage loads body under the runtime and waits for the validity
// module the headless field marker loads.
func validityPage(t *testing.T, body string) (run func(...chromedp.Action)) {
	t.Helper()
	page := `<!doctype html><html><head><title>validity</title></head><body>` + body +
		`<script src="/__gofastr/runtime.js"></script></body></html>`
	srv := formErrorsServer(t, page, func(int32) (int, string) {
		return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{}}`
	})
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL+"/"),
		chromedp.Poll(`window.__gofastr?.loadedModules?.validity === true`, nil, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("the validity module did not load for a headless field: %v", err)
	}
	return func(a ...chromedp.Action) {
		t.Helper()
		if err := chromedp.Run(ctx, a...); err != nil {
			t.Fatal(err)
		}
	}
}

func invalidOf(id string, out *string) chromedp.Action {
	return chromedp.Evaluate(`String(document.getElementById('`+id+`').getAttribute('aria-invalid'))`, out)
}

// A field the reader leaves invalid reads aria-invalid="true", only
// once they leave it (typing into an untouched field marks nothing),
// and fixing it while typing clears the mark to "false" (#704).
func TestUserInvalidMirrorsToAriaInvalid(t *testing.T) {
	run := validityPage(t, `<form id="f">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" type="email" required></div>
  <div data-hui-field=""><label for="city">City</label><input id="city" name="city"></div>
</form>`)
	var before, typing, left, fixed string
	run(
		invalidOf("email", &before),
		chromedp.Click(`#email`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "nope", chromedp.ByID),
		invalidOf("email", &typing),
		chromedp.KeyEvent(kb.Tab),
		chromedp.Poll(`document.getElementById('email').getAttribute('aria-invalid') === 'true'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		invalidOf("email", &left),
		chromedp.Click(`#email`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "@example.com", chromedp.ByID),
		chromedp.Poll(`document.getElementById('email').getAttribute('aria-invalid') !== 'true'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		invalidOf("email", &fixed),
	)
	if before != "null" || typing != "null" {
		t.Errorf("aria-invalid ran ahead of the browser: on load %q, while typing %q; want neither set", before, typing)
	}
	if left != "true" {
		t.Errorf("a field left invalid reads aria-invalid=%q, want true", left)
	}
	if fixed != "false" {
		t.Errorf("a field fixed while typing reads aria-invalid=%q, want false", fixed)
	}
}

// A submit the browser blocks marks every refused control at once; a
// script's checkValidity(), which no reader caused, marks nothing.
func TestBlockedSubmitMarksRefusedControls(t *testing.T) {
	run := validityPage(t, `<form id="f">
  <div data-hui-field=""><label for="name">Name</label><input id="name" name="name" required></div>
  <div data-hui-field=""><label for="code">Code</label><input id="code" name="code" required></div>
  <button id="go">Save</button>
</form>`)
	var scripted, name, code string
	run(
		chromedp.Evaluate(`document.getElementById('f').checkValidity()`, nil),
		chromedp.Sleep(50*time.Millisecond),
		invalidOf("name", &scripted),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`document.getElementById('code').getAttribute('aria-invalid') === 'true'`, nil, chromedp.WithPollingTimeout(2*time.Second)),
		invalidOf("name", &name),
		invalidOf("code", &code),
	)
	if scripted != "null" {
		t.Errorf("a script's checkValidity marked a field nobody touched: aria-invalid=%q", scripted)
	}
	if name != "true" || code != "true" {
		t.Errorf("a blocked submit marked name=%q code=%q, want both true", name, code)
	}
}

// A server's verdict stands while its words describe the control: a
// value the browser accepts does not flip it to "false".
func TestServerVerdictStandsOverNativeValid(t *testing.T) {
	run := validityPage(t, `<form id="f">
  <div data-hui-field=""><label for="slug">Slug</label><input id="slug" name="slug" aria-invalid="true" aria-describedby="slug-error" value="taken"><p id="slug-error" data-hui-field-error="">Already in use.</p></div>
  <div data-hui-field=""><label for="other">Other</label><input id="other" name="other"></div>
</form>`)
	var got string
	run(
		chromedp.Click(`#slug`, chromedp.ByID),
		chromedp.SendKeys(`#slug`, "-2", chromedp.ByID),
		chromedp.Click(`#other`, chromedp.ByID),
		chromedp.Sleep(100*time.Millisecond),
		invalidOf("slug", &got),
	)
	if got != "true" {
		t.Errorf("a native valid overwrote the server's verdict: aria-invalid=%q, want true", got)
	}
}
