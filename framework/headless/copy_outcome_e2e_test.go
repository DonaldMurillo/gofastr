package headless

// The copy says what the clipboard answered, not what the click hoped.

import (
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

const copyWrapper = `<pre id="code">npm i gofastr</pre>` + copyControl

const copyControl = `<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy" ` +
	`data-hui-copy-sentence="Copied {{name}}" data-hui-copy-failed="Could not copy {{name}}" data-hui-copy-failed-label="Not copied" data-hui-copy-name="the command">` +
	`<button id="cp" type="button" data-hui-copy-label>Copy</button><span data-hui-copy-status></span></span>`

// A copy whose clipboard write is refused (a declined permission) or
// impossible (no clipboard on an insecure origin) does not say it
// copied: the wrapper reads failed, never done, the label shows the
// failed label, and the status says the failure sentence. A write that
// resolves says Copied.
func TestE2E_CopySaysWhatTheClipboardAnswered(t *testing.T) {
	for _, c := range []struct{ name, clipboard, want string }{
		{"refused", `{writeText: () => Promise.reject(new DOMException('denied', 'NotAllowedError'))}`, "Not copied|failed|Could not copy the command"},
		{"missing", `undefined`, "Not copied|failed|Could not copy the command"},
		{"resolved", `{writeText: () => Promise.resolve()}`, "Copied|done|Copied the command"},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := startBehaviorServer(t, copyWrapper)
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
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

// A form field is copied by its current value, which an edit changes
// after the page rendered, not by its (empty) text content.
func TestE2E_CopyReadsAFieldsValue(t *testing.T) {
	for _, c := range []struct{ name, field string }{
		{"input", `<input id="code" value="as rendered">`},
		{"textarea", `<textarea id="code">as rendered</textarea>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := startBehaviorServer(t, c.field+
				`<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy">`+
				`<button id="cp" type="button" data-hui-copy-label>Copy</button><span data-hui-copy-status></span></span>`)
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
				t.Fatal("module never loaded")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__copied = null;
				Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: (t) => { window.__copied = t; return Promise.resolve(); }}});
				document.getElementById('code').value = 'as edited';
				document.getElementById('cp').click()`, nil)); err != nil {
				t.Fatal(err)
			}
			time.Sleep(200 * time.Millisecond)
			if got := evalString(t, ctx, `String(window.__copied)`); got != "as edited" {
				t.Fatalf("copied %q, want the field's current value", got)
			}
		})
	}
}

// copyToastWrapper is the copy wrapper with a toast-on-copy config on
// its button, the way ui.CopyButton renders ToastOnCopy.
const copyToastWrapper = `<pre id="code">npm i gofastr</pre>` +
	`<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy" ` +
	`data-hui-copy-sentence="Copied {{name}}" data-hui-copy-failed="Could not copy {{name}}" data-hui-copy-name="the command">` +
	`<button id="cp" type="button" data-hui-copy-label data-hui-copy-toast='{"title":"Copied it","variant":"success"}'>Copy</button>` +
	`<span data-hui-copy-status></span></span>`

// The copy control is headless-copy's and the toast runtime
// headless-feedback's: a copy that toasts reaches the runtime across
// the split. With a stack on the page the toast is a stack row; with
// none (only the copy wrapper's marker loads either module) it lands
// in the kernel's fallback region rather than vanishing.
func TestE2E_CopyToastCrossesTheModuleSplit(t *testing.T) {
	for _, c := range []struct {
		name, body, row string
	}{
		{"with a stack", copyToastWrapper + string(ToastStack(ToastStackProps{ID: "stack", Label: "Notifications"}, nil)),
			`[data-hui-toast-stack] [data-hui-toast-id] [data-hui-toast-title]`},
		{"without a stack", copyToastWrapper, `[data-cui-toast-fallback] strong`},
	} {
		t.Run(c.name, func(t *testing.T) {
			b := startBehaviorServer(t, c.body)
			ctx := behaviorPage(t, b)
			if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
				t.Fatal("the copy marker never loaded headless-copy")
			}
			if err := chromedp.Run(ctx, chromedp.Evaluate(`Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: () => Promise.resolve()}}); document.getElementById('cp').click()`, nil)); err != nil {
				t.Fatal(err)
			}
			if !pollTrue(ctx, `(function(){var t=document.querySelector('`+c.row+`');return !!t && t.textContent==='Copied it';})()`) {
				t.Fatalf("no toast after a copy that toasts; body:\n%s", evalString(t, ctx, `document.body.innerHTML`))
			}
		})
	}
}

// A password field is never copied: its value would land in the
// clipboard history. The click writes nothing.
func TestE2E_CopyNeverReadsAPasswordField(t *testing.T) {
	b := startBehaviorServer(t, `<input id="code" type="password" value="hunter2">`+
		`<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy">`+
		`<button id="cp" type="button" data-hui-copy-label>Copy</button><span data-hui-copy-status></span></span>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__copied = null;
		Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: (t) => { window.__copied = t; return Promise.resolve(); }}});
		document.getElementById('cp').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := evalString(t, ctx, `String(window.__copied)`); got != "null" {
		t.Fatalf("a password field was copied: %q", got)
	}
}

// A field's value is copied exactly, including leading spaces and a trailing newline.
func TestE2E_CopyKeepsAFieldsWhitespace(t *testing.T) {
	b := startBehaviorServer(t, `<textarea id="code"></textarea>`+
		`<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy">`+
		`<button id="cp" type="button" data-hui-copy-label>Copy</button><span data-hui-copy-status></span></span>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__copied = null;
		Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: (t) => { window.__copied = t; return Promise.resolve(); }}});
		document.getElementById('code').value = '  indented\n';
		document.getElementById('cp').click()`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if got := evalString(t, ctx, `JSON.stringify(window.__copied)`); got != `"  indented\n"` {
		t.Fatalf("copied %s, want the field's value with its whitespace", got)
	}
}

func TestCopyPreservesPreformattedText(t *testing.T) {
	const source = "  line 1\n  line 2\n"
	for _, tc := range []struct{ name, target, want string }{
		{"pre", `<pre id="code">` + source + `</pre>`, source},
		{"ordinary text", `<div id="code">` + source + `</div>`, "line 1 line 2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := behaviorPage(t, startBehaviorServer(t, tc.target+copyControl))
			if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
				t.Fatal("module never loaded")
			}
			var got string
			if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
				let copied = '';
				Object.defineProperty(navigator, 'clipboard', {value: {writeText: text => { copied = text; return Promise.resolve(); }}});
				document.getElementById('cp').click();
				return copied;
			})()`, &got)); err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("clipboard got %q, want %q", got, tc.want)
			}
		})
	}
}

// A refused copy right after a successful one cancels the success's
// restore timer, so it restores the label itself, even on a wrapper
// with no failed label of its own.
func TestE2E_CopyFailureAfterSuccessRestoresTheLabel(t *testing.T) {
	b := startBehaviorServer(t, `<pre id="code">npm i gofastr</pre>`+
		`<span data-hui-copy data-hui-copy-target="code" data-hui-copy-copied="Copied" data-hui-copy-back="Copy">`+
		`<button id="cp" type="button" data-hui-copy-label>Copy</button><span data-hui-copy-status></span></span>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, controlsLoadedExpr(CopyBehaviorName)) {
		t.Fatal("module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`
		Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: () => Promise.resolve()}});
		document.getElementById('cp').click();
		setTimeout(() => {
			Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: () => Promise.reject(new DOMException('denied', 'NotAllowedError'))}});
			document.getElementById('cp').click();
		}, 200)`, nil)); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * time.Second)
	got := evalString(t, ctx, `document.getElementById('cp').textContent + '|' + (document.querySelector('[data-hui-copy]').getAttribute('data-hui-copy-state') || 'none')`)
	if got != "Copy|none" {
		t.Fatalf("[label|state] = %q after a success then a failure, want Copy|none", got)
	}
}
