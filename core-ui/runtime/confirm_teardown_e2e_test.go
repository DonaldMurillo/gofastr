package runtime

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// Each close path is checked inside the task that closed the confirm.
func TestConfirmTeardownRunsInTheClosingTask(t *testing.T) {
	base := startPollServer(t, `<!doctype html><html><head></head><body>
<button id="b">b</button>
`+confirmTemplate+`
<script src="/__gofastr/runtime.js"></script></body></html>`, nil)

	probe := func(close string) string {
		return `(async () => {
	const G = window.__gofastr;
	await G.loadModule('confirm');
	let answer = 'pending';
	G.askWords({message: 'Sure?'}).then((v) => { answer = v; });
	while (!document.querySelector('body > dialog[open]')) await new Promise((r) => setTimeout(r, 10));
	const d = document.querySelector('body > dialog');
	` + close + `;
	for (let i = 0; i < 5; i++) await Promise.resolve();
	return {open: d.open, attached: d.isConnected, answer: answer};
})()`
	}
	cases := []struct {
		name, close string
		answer      any
	}{
		{"close()", `d.close()`, false},
		{"accept", `d.querySelector('[data-cui-confirm-part="accept"]').click()`, true},
		{"cancel", `d.querySelector('[data-cui-confirm-part="cancel"]').click()`, false},
		{"requestClose", `d.requestClose ? d.requestClose() : d.close()`, false},
	}
	ctx := chromedptest.Context(t)
	for _, c := range cases {
		var got map[string]any
		if err := chromedp.Run(ctx, chromedp.Navigate(base+"/"), chromedp.WaitVisible(`#b`, chromedp.ByID), chromedp.Evaluate(probe(c.close), &got, func(p *runtime.EvaluateParams) *runtime.EvaluateParams {
			return p.WithAwaitPromise(true)
		})); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if got["open"] != false || got["attached"] != false || got["answer"] != c.answer {
			t.Errorf("%s: closed confirm %v; want it removed and answered %v in the closing task", c.name, got, c.answer)
		}
	}
}
