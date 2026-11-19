package runtime

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// focusAfterRefusal loads body under the runtime, answers every
// /rpc/save with a 422 carrying fields, runs act, waits for the error
// node with id waitErr and returns the id of the focused element.
func focusAfterRefusal(t *testing.T, body, fields, waitErr string, hold <-chan struct{}, act ...chromedp.Action) string {
	t.Helper()
	page := `<!doctype html><html><head><title>focus</title></head><body>` + body +
		`<script src="/__gofastr/runtime.js"></script></body></html>`
	// done unblocks a held answer when the test ends early, so the
	// server's Close, registered first and run last, never waits on it.
	done := make(chan struct{})
	srv := formErrorsServer(t, page, func(int32) (int, string) {
		if hold != nil {
			select {
			case <-hold:
			case <-done:
			}
		}
		return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":` + fields + `}`
	})
	t.Cleanup(func() { close(done) })
	ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
	var focused string
	steps := append([]chromedp.Action{chromedp.Navigate(srv.URL + "/")}, act...)
	steps = append(steps,
		chromedp.Poll(`!!document.getElementById('`+waitErr+`')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
		chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &focused))
	if err := chromedp.Run(ctx, steps...); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return focused
}

func clickSave() chromedp.Action {
	return chromedp.Tasks{chromedp.WaitVisible(`#go`, chromedp.ByID), chromedp.Click(`#go`, chromedp.ByID)}
}

// A refused submit moves focus to the first refused control in the
// form's order, so a keyboard or screen-reader user lands on the field
// to fix (#704).
func TestRefusedSubmitFocusesFirstInvalidField(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="name">Name</label><input id="name" name="name" value="ok"></div>
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <div data-hui-field=""><label for="city">City</label><input id="city" name="city" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"city":["is required"],"email":["is required"]}`, "city-error", nil, clickSave())
	if got != "email" {
		t.Errorf("focus after the refused submit is on %q, want the first refused field %q", got, "email")
	}
}

// A form that submits as the reader types keeps focus where the reader
// is typing when the server refuses another field.
func TestRefusedInputDispatchKeepsFocus(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST" data-cui-rpc-trigger="input" data-cui-rpc-debounce-ms="10">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <div data-hui-field=""><label for="city">City</label><input id="city" name="city" value=""></div>
</form>`, `{"email":["is required"]}`, "email-error", nil,
		chromedp.WaitVisible(`#city`, chromedp.ByID), chromedp.SendKeys(`#city`, "Lisbon", chromedp.ByID))
	if got != "city" {
		t.Errorf("focus after an as-you-type refusal is on %q, want it left on %q", got, "city")
	}
}

// A control associated from outside the form (form="f") is refused like
// any other, and comes first when it is first in tree order.
func TestRefusedSubmitFocusesAnExternalControl(t *testing.T) {
	got := focusAfterRefusal(t, `<div data-hui-field=""><label for="code">Code</label><input id="code" name="code" form="f" value=""></div>
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"email":["is required"],"code":["is required"]}`, "code-error", nil, clickSave())
	if got != "code" {
		t.Errorf("focus after the refused submit is on %q, want the external control %q", got, "code")
	}
}

// A reader who moved focus out of the form while the submit was in
// flight keeps it there when the refusal lands.
func TestLateRefusalLeavesFocusOutsideTheForm(t *testing.T) {
	release := make(chan struct{})
	got := focusAfterRefusal(t, `<input id="search" aria-label="Search">
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"email":["is required"]}`, "email-error", release,
		clickSave(),
		chromedp.Focus(`#search`, chromedp.ByID),
		chromedp.ActionFunc(func(context.Context) error { close(release); return nil }))
	if got != "search" {
		t.Errorf("focus after a late refusal is on %q, want it left on %q", got, "search")
	}
}

// A control that dispatches on click (a segmented choice inside a form)
// is not a submit: a refusal does not move focus to the refused field.
func TestRefusedClickDispatchKeepsFocus(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="button" id="plan" data-cui-rpc="/rpc/save" name="plan" value="pro">Pro</button>
</form>`, `{"email":["is required"]}`, "email-error", nil,
		chromedp.WaitVisible(`#plan`, chromedp.ByID), chromedp.Click(`#plan`, chromedp.ByID))
	if got == "email" {
		t.Errorf("a click-dispatched refusal moved focus to the refused field %q", got)
	}
}

// A refused hidden value (a combobox's) cannot take focus, so focus
// goes to the next refused control that can.
func TestRefusedSubmitSkipsAHiddenInput(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="owner-q">Owner</label><input id="owner-q" value=""><input type="hidden" id="owner" name="owner" value=""></div>
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"owner":["is required"],"email":["is required"]}`, "email-error", nil, clickSave())
	if got != "email" {
		t.Errorf("focus after the refused submit is on %q, want the first focusable refused field %q", got, "email")
	}
}

// A refused radio group takes focus on its checked radio, not on the
// last one, which is the radio report marks.
func TestRefusedSubmitFocusesTheCheckedRadio(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <fieldset data-hui-field=""><legend>Plan</legend>
    <input type="radio" id="p-free" name="plan" value="free">
    <input type="radio" id="p-pro" name="plan" value="pro" checked>
    <input type="radio" id="p-team" name="plan" value="team">
  </fieldset>
  <button type="submit" id="go">Save</button>
</form>`, `{"plan":["is not available"]}`, "p-team-error", nil, clickSave())
	if got != "p-pro" {
		t.Errorf("focus after the refused submit is on %q, want the checked radio %q", got, "p-pro")
	}
}

// A refused disabled control cannot take focus, so focus goes to the
// next refused control that can.
func TestRefusedSubmitSkipsADisabledControl(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="owner">Owner</label><input id="owner" name="owner" value="ann" disabled></div>
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"owner":["is required"],"email":["is required"]}`, "email-error", nil, clickSave())
	if got != "email" {
		t.Errorf("focus after the refused submit is on %q, want the first enabled refused field %q", got, "email")
	}
}

// A retry clears what the last refusal marked on a control associated
// from outside the form, so focus follows only the newest refusal: in a
// field, or as a bare choice whose error sits right after its label.
func TestRetryClearsAnExternalControlsError(t *testing.T) {
	for name, outside := range map[string]string{
		"field":       `<div data-hui-field=""><label for="code">Code</label><input id="code" name="code" form="f" value=""></div>`,
		"bare choice": `<label data-hui-choice=""><input type="checkbox" id="code" name="code" form="f" value="yes"> Agree</label>`,
	} {
		t.Run(name, func(t *testing.T) {
			page := `<!doctype html><html><head><title>focus</title></head><body>` + outside + `
<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>
<script src="/__gofastr/runtime.js"></script></body></html>`
			srv := formErrorsServer(t, page, func(n int32) (int, string) {
				if n == 1 {
					return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"code":["is required"]}}`
				}
				return http.StatusUnprocessableEntity, `{"error":"validation failed","fields":{"email":["is required"]}}`
			})
			ctx := chromedptest.Context(t, chromedptest.Timeout(60*time.Second))
			var focused, codeState string
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL+"/"),
				clickSave(),
				chromedp.Poll(`!!document.getElementById('code-error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Click(`#go`, chromedp.ByID),
				chromedp.Poll(`!!document.getElementById('email-error')`, nil, chromedp.WithPollingTimeout(10*time.Second)),
				chromedp.Evaluate(`document.activeElement && document.activeElement.id`, &focused),
				chromedp.Evaluate(`const c = document.getElementById('code'); (c.getAttribute('aria-invalid') || '') + '|' + !!document.getElementById('code-error') + '|' + (c.getAttribute('aria-describedby') || '')`, &codeState),
			); err != nil {
				t.Fatalf("chromedp: %v", err)
			}
			if focused != "email" {
				t.Errorf("focus after the retry is on %q, want %q, the only field the newest answer refused", focused, "email")
			}
			if codeState != "|false|" {
				t.Errorf("the external control after the retry reads %q (aria-invalid|error node|describedby), want all cleared", codeState)
			}
		})
	}
}

// A control in a disabled fieldset is disabled though its own disabled
// property is false, so focus skips it too.
func TestRefusedSubmitSkipsADisabledFieldset(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <fieldset disabled><div data-hui-field=""><label for="owner">Owner</label><input id="owner" name="owner" value=""></div></fieldset>
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"owner":["is required"],"email":["is required"]}`, "email-error", nil, clickSave())
	if got != "email" {
		t.Errorf("focus after the refused submit is on %q, want the first enabled refused field %q", got, "email")
	}
}

// A refused radio group whose last radio, the one report marks, is
// disabled still takes focus on an enabled radio.
func TestRefusedRadioGroupWithADisabledLastRadio(t *testing.T) {
	got := focusAfterRefusal(t, `<form id="f" data-cui-rpc="/rpc/save" data-cui-rpc-method="POST">
  <fieldset data-hui-field=""><legend>Plan</legend>
    <input type="radio" id="p-free" name="plan" value="free">
    <input type="radio" id="p-team" name="plan" value="team" disabled>
  </fieldset>
  <div data-hui-field=""><label for="email">Email</label><input id="email" name="email" value=""></div>
  <button type="submit" id="go">Save</button>
</form>`, `{"plan":["is required"],"email":["is required"]}`, "email-error", nil, clickSave())
	if got != "p-free" {
		t.Errorf("focus after the refused submit is on %q, want the enabled radio %q", got, "p-free")
	}
}
