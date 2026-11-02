package headless

import (
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// A named Counter's input carried data-cui-signal and
// data-cui-signal-attr="value" but no data-cui-signal-mode. The kernel
// reads `mode = getAttribute('data-cui-signal-mode') || 'text'`
// (core-ui/runtime/frag/signals.js), so the input was a TEXT binding:
// each step set the void input's textContent, its value never moved,
// and the form submitted the server's number. The package's own
// binding spelling (Bind.attrs) always writes the mode.
func TestNamedCounterBindsTheValueAttribute(t *testing.T) {
	h := string(Counter(CounterProps{Signal: "seats", Label: "Seats", Name: "seats", Value: 4}, nil))
	input := regexp.MustCompile(`<input[^>]*>`).FindString(h)
	for k, v := range (Bind{Signal: "seats", Mode: "attr", Attr: "value"}).attrs() {
		if !strings.Contains(input, k+`="`+v+`"`) {
			t.Errorf("the named counter's input lacks %s=%q, so the runtime binds it as text: %s", k, v, input)
		}
	}
}

// The browser proof: a step moves the input's value (what the form
// submits), and the input keeps its spinbutton role.
func TestE2E_NamedCounterStepMovesTheSubmittedValue(t *testing.T) {
	body := `<form id="cf">` + string(Counter(CounterProps{Signal: "seats", Label: "Seats", Name: "seats", Value: 4}, nil)) + `</form>`
	b := startBehaviorServer(t, body)
	ctx := behaviorPage(t, b)
	// The page's store seed (a store.Slice in the styled layer) is
	// what the step reads; this page has none, so seed it the way the
	// SSR seed would.
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`window.__gofastr.setSignal('seats', 4); document.querySelector('[data-cui-signal-inc="seats"]').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	if !pollTrue(ctx, `new FormData(document.getElementById('cf')).get('seats') === '5'`) {
		var got string
		_ = chromedp.Run(ctx, chromedp.Evaluate(`String(new FormData(document.getElementById('cf')).get('seats')) + ' text=' + document.querySelector('input[name=seats]').textContent`, &got))
		t.Fatalf("after one step the form submits seats=%s, want 5", got)
	}
	var role string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('input[name=seats]').getAttribute('role') || ''`, &role)); err != nil {
		t.Fatal(err)
	}
	if role != "" {
		t.Fatalf("the counter's input was given role=%q over its spinbutton role", role)
	}
}
