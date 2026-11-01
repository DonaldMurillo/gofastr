package headless

// The copy says what the clipboard answered, not what the click hoped.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const copyWrapper = `<pre id="code">npm i gofastr</pre>` +
	`<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy" ` +
	`data-hui-copy-sentence="Copied {{name}}" data-hui-copy-failed="Could not copy {{name}}" data-hui-copy-name="the command">` +
	`<button id="cp" type="button" data-hui-copy-label>Copy</button><span data-hui-copy-status></span></span>`

// A copy whose clipboard write is refused (a declined permission) or
// impossible (no clipboard on an insecure origin) does not say it
// copied: the label stays, the wrapper never reads done, and the
// status says the failure sentence. A write that resolves says Copied.
func TestE2E_CopySaysWhatTheClipboardAnswered(t *testing.T) {
	for _, c := range []struct{ name, clipboard, want string }{
		{"refused", `{writeText: () => Promise.reject(new DOMException('denied', 'NotAllowedError'))}`, "Copy|none|Could not copy the command"},
		{"missing", `undefined`, "Copy|none|Could not copy the command"},
		{"resolved", `{writeText: () => Promise.resolve()}`, "Copied|done|Copied the command"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := startBehaviorServer(t, copyWrapper)
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, controlsLoadedExpr("headless-feedback")) {
				t.Fatal("module never loaded")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`Object.defineProperty(navigator, 'clipboard', {configurable: true, value: `+c.clipboard+`}); document.getElementById('cp').click()`, nil)); err != nil {
				t.Fatal(err)
			}
			time.Sleep(300 * time.Millisecond)
			got := evalString(t, ctx, `document.getElementById('cp').textContent + '|' + (document.querySelector('[data-hui-copy]').getAttribute('data-hui-copy-state') || 'none') + '|' + document.querySelector('[data-hui-copy-status]').textContent`)
			if got != c.want {
				t.Fatalf("[label|state|status] = %q, want %q", got, c.want)
			}
		})
	}
}
