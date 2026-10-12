package headless

import (
	"testing"

	"github.com/chromedp/chromedp"
)

// A JSON textarea says its text is not JSON as it is typed: the control
// reads invalid, its custom validity is its own sentence (the form's
// submit stops on it), and valid or empty text clears both.
func TestE2E_TextareaJSONCheck(t *testing.T) {
	area := Textarea(TextareaProps{Name: "meta", ID: "meta", JSON: "Enter valid JSON"}, nil)
	b := startBehaviorServer(t, `<form id="f">`+string(area)+`</form>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules['headless-controls'])`) {
		t.Fatal("the JSON hook never loaded headless-controls")
	}
	type state struct {
		Valid   bool   `json:"valid"`
		Invalid string `json:"invalid"`
		Msg     string `json:"msg"`
	}
	set := func(v string) state {
		t.Helper()
		var s state
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const el = document.getElementById('meta');
			el.value = `+"`"+v+"`"+`;
			el.dispatchEvent(new Event('input', {bubbles: true}));
			return {valid: document.getElementById('f').checkValidity(), invalid: el.getAttribute('aria-invalid') || '', msg: el.validationMessage};
		})()`, &s)); err != nil {
			t.Fatal(err)
		}
		return s
	}
	if s := set(`{"plan": `); s.Valid || s.Invalid != "true" || s.Msg != "Enter valid JSON" {
		t.Errorf("broken JSON = %+v", s)
	}
	if s := set(`{"plan": "pro"}`); !s.Valid || s.Invalid != "" {
		t.Errorf("valid JSON = %+v", s)
	}
	if s := set(``); !s.Valid {
		t.Errorf("empty text = %+v", s)
	}
}
