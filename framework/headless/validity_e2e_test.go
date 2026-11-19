package headless

import (
	"context"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// validityPage loads body and waits for headless-validity, which a
// Field or Choice marker loads.
func validityPage(t *testing.T, body string) func(...chromedp.Action) {
	t.Helper()
	ctx := behaviorPage(t, startBehaviorServer(t, body))
	if !pollTrue(ctx, controlsLoadedExpr(ValidityBehaviorName)) {
		t.Fatal("a headless field did not load headless-validity")
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

func waitFor(js string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		return chromedp.Poll(js, nil, chromedp.WithPollingTimeout(2*time.Second)).Do(ctx)
	})
}

// A field the reader leaves invalid reads aria-invalid="true", only
// once they leave it (typing into an untouched field marks nothing),
// and fixing it while typing clears the mark to "false" (#704).
func TestE2E_UserInvalidMirrorsToAriaInvalid(t *testing.T) {
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
		waitFor(`document.getElementById('email').getAttribute('aria-invalid') === 'true'`),
		invalidOf("email", &left),
		chromedp.Click(`#email`, chromedp.ByID),
		chromedp.SendKeys(`#email`, "@example.com", chromedp.ByID),
		waitFor(`document.getElementById('email').getAttribute('aria-invalid') !== 'true'`),
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
func TestE2E_BlockedSubmitMarksRefusedControls(t *testing.T) {
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
		waitFor(`document.getElementById('code').getAttribute('aria-invalid') === 'true'`),
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
func TestE2E_ServerVerdictStandsOverNativeValid(t *testing.T) {
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

// A radio group has one verdict: after a blocked submit every radio is
// "true", and picking one makes every radio "false" (#704 review).
func TestE2E_RadioGroupSharesOneVerdict(t *testing.T) {
	run := validityPage(t, `<form id="f">
  <fieldset data-hui-field=""><legend>Plan</legend>
    <label data-hui-choice=""><input type="radio" id="pa" name="plan" value="a" required> Alpha</label>
    <label data-hui-choice=""><input type="radio" id="pb" name="plan" value="b"> Beta</label>
    <label data-hui-choice=""><input type="radio" id="pc" name="plan" value="c"> Gamma</label>
  </fieldset>
  <button id="go">Save</button>
</form>`)
	var refused, picked string
	all := `['pa','pb','pc'].map((id) => String(document.getElementById(id).getAttribute('aria-invalid'))).join(',')`
	run(
		chromedp.Click(`#go`, chromedp.ByID),
		waitFor(all+` === 'true,true,true'`),
		chromedp.Evaluate(all, &refused),
		chromedp.Click(`#pb`, chromedp.ByID),
		waitFor(all+` === 'false,false,false'`),
		chromedp.Evaluate(all, &picked),
	)
	if refused != "true,true,true" || picked != "false,false,false" {
		t.Errorf("radio verdicts: after the blocked submit %s, after a pick %s; want all true, then all false", refused, picked)
	}
}

// A control with no id is not described by its error node, so the
// server's verdict is read from its field's own error node instead.
func TestE2E_ServerVerdictStandsWithoutAnID(t *testing.T) {
	run := validityPage(t, `<form id="f">
  <div data-hui-field=""><label>Slug <input name="slug" aria-invalid="true" value="taken"></label><p data-hui-field-error="live">Already in use.</p></div>
  <div data-hui-field=""><label for="other">Other</label><input id="other" name="other"></div>
</form>`)
	var got string
	run(
		chromedp.Click(`[name=slug]`, chromedp.ByQuery),
		chromedp.SendKeys(`[name=slug]`, "-2", chromedp.ByQuery),
		chromedp.Click(`#other`, chromedp.ByID),
		chromedp.Sleep(100*time.Millisecond),
		chromedp.Evaluate(`String(document.querySelector('[name=slug]').getAttribute('aria-invalid'))`, &got),
	)
	if got != "true" {
		t.Errorf("a native valid overwrote the server's verdict on a control with no id: aria-invalid=%q", got)
	}
}
