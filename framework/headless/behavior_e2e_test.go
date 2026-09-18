package headless

// Browser coverage for the behaviour module: the kernel loads it on a
// marker and not without one, each behaviour does to the rendered
// markup what its component's doc comment promises, and markup that
// arrives after load is armed by the kernel's insertion scan. The
// harness mirrors core-ui/runtime/behavior_e2e_test.go: an httptest
// server that serves the real runtime.js, the module the way the host
// does, and one page per test whose body is the rendered component at
// the nil Classes. The real registration is used, never
// registry.IsolateForTest: these tests exist to prove the registration
// itself serves.

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/browserpath"
)

// One shared browser for the whole file: an allocator per test would
// boot Chrome eleven times for what is one surface.
var behaviorAlloc struct {
	once sync.Once
	ctx  context.Context
}

func behaviorBrowserCtx(t *testing.T) context.Context {
	t.Helper()
	if testing.Short() {
		t.Skip("browser E2E disabled in short mode")
	}
	behaviorAlloc.once.Do(func() {
		execPath, ok := browserpath.Find()
		if !ok {
			return
		}
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(execPath),
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-sandbox", true),
			chromedp.WSURLReadTimeout(90*time.Second),
		)
		allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
		behaviorAlloc.ctx = allocCtx
		// The browser outlives every test; canceling would kill later
		// ones, so the cancel is deliberately dropped (the process dies
		// with the test binary).
		_ = cancel
	})
	if behaviorAlloc.ctx == nil {
		t.Skip("browser E2E requires Chrome, Chromium, or Edge")
	}
	ctx, cancel := chromedp.NewContext(behaviorAlloc.ctx)
	t.Cleanup(cancel)
	tctx, tcancel := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(tcancel)
	return tctx
}

// behaviorServer serves the runtime, the headless module (counting
// every fetch), every other module by name, and one page built from
// body. The page carries the inline #gofastr-behaviors block in the
// head, the shape an export or the embed frame ships.
type behaviorServer struct {
	srv  *httptest.Server
	hits atomic.Int32
}

func startBehaviorServer(t *testing.T, body string, extra ...func(mux *http.ServeMux)) *behaviorServer {
	t.Helper()
	js, err := runtime.RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	mod, ok := runtime.Module(BehaviorName)
	if !ok {
		t.Fatalf("%s is not served by runtime.Module: the registration is not live in this binary", BehaviorName)
	}
	block := runtime.BehaviorsJSON()
	if block == nil {
		t.Fatal("runtime.BehaviorsJSON returned nil: the headless behaviour is not registered in this binary")
	}
	b := &behaviorServer{}
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	// Mutation endpoints for the action tests: a real 204 commits, a
	// real 422 rolls back. The action primitive forwards these the
	// way it forwards any app endpoint, so the announcement is driven
	// by a genuine response, not a synthetic event.
	mux.HandleFunc("/__hui/ok", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/__hui/fail", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusUnprocessableEntity)
	})
	mux.HandleFunc("/__gofastr/runtime/headless.js", func(w http.ResponseWriter, r *http.Request) {
		b.hits.Add(1)
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(mod))
	})
	// Any other module the kernel decides to load must get JavaScript,
	// not page HTML: an HTML body in a <script> is an uncaught
	// SyntaxError with nothing to do with the behaviour under test.
	mux.HandleFunc("/__gofastr/runtime/", func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/__gofastr/runtime/"), ".js")
		w.Header().Set("Content-Type", "application/javascript")
		if src, ok := runtime.Module(name); ok {
			w.Write([]byte(src))
			return
		}
		http.NotFound(w, r)
	})
	for _, add := range extra {
		add(mux)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><html><head><title>headless</title>`+
			`<script type="application/json" id="gofastr-behaviors">%s</script></head><body>`+
			`<main role="main"><span id="ready">ready</span>%s</main>`+
			`<script src="/__gofastr/runtime.js"></script></body></html>`, block, body)
	})
	b.srv = httptest.NewServer(mux)
	t.Cleanup(b.srv.Close)
	return b
}

// behaviorPage navigates and waits for the parsed page.
func behaviorPage(t *testing.T, b *behaviorServer) context.Context {
	t.Helper()
	ctx := behaviorBrowserCtx(t)
	if err := chromedp.Run(ctx,
		chromedp.Navigate(b.srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	return ctx
}

// pollTrue evaluates js (a boolean expression) until it is true or the
// bounded budget runs out. Polls instead of sleeping a fixed settle.
func pollTrue(ctx context.Context, js string) bool {
	for range 40 {
		var v bool
		if err := chromedp.Run(ctx, chromedp.Evaluate(js, &v)); err == nil && v {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

const moduleLoadedExpr = `!!(window.__gofastr && window.__gofastr.loadedModules && window.__gofastr.loadedModules.headless)`

// bootSettledExpr is true once the kernel's initial pass has run:
// readyState left "loading" means DOMContentLoaded fired, and the
// module scan it triggers is synchronous, so a marker on the page has
// started its fetch by the time this flips.
const bootSettledExpr = `(document.readyState === 'interactive' || document.readyState === 'complete') && !!window.__gofastr`

// A page whose DOM carries a marker fetches the module exactly once,
// and a page without one never does. The second half is the waste the
// marker contract exists to prevent: bytes and a binding pass spent on
// markup that cannot use them.
func TestE2E_ModuleLoadsOnAMarkerOnly(t *testing.T) {
	b := startBehaviorServer(t,
		string(Password(PasswordProps{Name: "token", ID: "token"}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the password marker never loaded the module")
	}
	if n := b.hits.Load(); n != 1 {
		t.Fatalf("headless.js fetched %d times for one marker, want exactly 1", n)
	}

	bare := startBehaviorServer(t, string(Badge(BadgeProps{Label: "running"}, nil)))
	ctx2 := behaviorPage(t, bare)
	if !pollTrue(ctx2, bootSettledExpr) {
		t.Fatal("the page never finished booting")
	}
	// A late fetch would land in this bounded window; none may.
	for range 5 {
		if n := bare.hits.Load(); n != 0 {
			t.Fatalf("a page with no marker fetched headless.js %d times: a marker is matching markup it cannot bind", n)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The reveal button retypes the input, swaps both its visible text and
// its accessible name, keeps focus and the caret, and swaps back.
func TestE2E_RevealRetypesAndRelabels(t *testing.T) {
	b := startBehaviorServer(t,
		string(Password(PasswordProps{Name: "token", ID: "token"}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.SendKeys(`[data-hui-affix-input]`, "hunter2", chromedp.ByQuery)); err != nil {
		t.Fatalf("typing into the password: %v", err)
	}

	var state struct {
		Type    string `json:"type"`
		Pressed string `json:"pressed"`
		Label   string `json:"label"`
		Text    string `json:"text"`
		Focused bool   `json:"focused"`
		At      int    `json:"at"`
		Len     int    `json:"len"`
	}
	probe := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const input = document.querySelector('[data-hui-affix-input]');
			const btn = document.querySelector('[data-hui-reveal]');
			return {
				type: input.type,
				pressed: btn.getAttribute('aria-pressed'),
				label: btn.getAttribute('aria-label'),
				text: btn.textContent,
				focused: document.activeElement === input,
				at: input.selectionStart === null ? -1 : input.selectionStart,
				len: input.value.length,
			};
		})()`, &state)); err != nil {
			t.Fatalf("reading the control's state: %v", err)
		}
	}

	if err := chromedp.Run(ctx, chromedp.Click(`[data-hui-reveal]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("clicking reveal: %v", err)
	}
	probe()
	if state.Type != "text" || state.Pressed != "true" {
		t.Fatalf("after the first click: type=%q aria-pressed=%q, want text and true", state.Type, state.Pressed)
	}
	if state.Label != "Hide password" || state.Text != "Hide" {
		t.Fatalf("after the first click: aria-label=%q text=%q, want the hide label and text", state.Label, state.Text)
	}
	if !state.Focused || state.At != state.Len || state.Len != 7 {
		t.Fatalf("after the first click: focused=%v caret=%d len=%d, want focus at the end of 7 characters", state.Focused, state.At, state.Len)
	}

	if err := chromedp.Run(ctx, chromedp.Click(`[data-hui-reveal]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("clicking reveal again: %v", err)
	}
	probe()
	if state.Type != "password" || state.Pressed != "false" {
		t.Fatalf("after the second click: type=%q aria-pressed=%q, want password and false", state.Type, state.Pressed)
	}
	if state.Label != "Show password" || state.Text != "Show" {
		t.Fatalf("after the second click: aria-label=%q text=%q, want the show label and text", state.Label, state.Text)
	}
}

// The swatch and the hex text stay one value, in both directions, and
// a value the picker cannot show is marked rather than rewritten.
func TestE2E_ColorSyncsBothWays(t *testing.T) {
	b := startBehaviorServer(t,
		string(Color(ColorProps{Name: "accent", ID: "accent", Value: "#10b981"}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}

	var state struct {
		Swatch  string `json:"swatch"`
		Text    string `json:"text"`
		Invalid bool   `json:"invalid"`
	}
	probe := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const shell = document.querySelector('[data-hui-color]');
			return {
				swatch: shell.querySelector('[data-hui-affix-swatch]').value,
				text: shell.querySelector('[data-hui-affix-input]').value,
				invalid: shell.hasAttribute('data-invalid'),
			};
		})()`, &state)); err != nil {
			t.Fatalf("reading the colour control: %v", err)
		}
	}

	// Real typing into the hex field, replacing what was selected.
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-color] [data-hui-affix-input]').select()`, nil),
		chromedp.SendKeys(`[data-hui-color] [data-hui-affix-input]`, "#abc", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("typing a short hex: %v", err)
	}
	probe()
	if state.Swatch != "#aabbcc" {
		t.Fatalf("typing #abc set the swatch to %q, want the expanded #aabbcc", state.Swatch)
	}
	if state.Invalid {
		t.Fatal("a parseable hex left data-invalid on the shell")
	}

	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-color] [data-hui-affix-input]').select()`, nil),
		chromedp.SendKeys(`[data-hui-color] [data-hui-affix-input]`, "var(--x)", chromedp.ByQuery),
	); err != nil {
		t.Fatalf("typing a token reference: %v", err)
	}
	probe()
	if !state.Invalid {
		t.Fatal("an unparseable non-empty value did not mark the shell data-invalid")
	}
	if state.Text != "var(--x)" {
		t.Fatalf("the unparseable value was rewritten to %q; it must stay verbatim", state.Text)
	}

	// From the swatch: there is no way to type into a colour picker, so
	// its input event is dispatched the way the picker itself fires it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const swatch = document.querySelector('[data-hui-affix-swatch]');
		swatch.value = '#00ff00';
		swatch.dispatchEvent(new Event('input', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatalf("picking a colour: %v", err)
	}
	probe()
	if state.Text != "#00FF00" {
		t.Fatalf("picking #00ff00 wrote %q to the text, want the uppercase #00FF00", state.Text)
	}
	if state.Invalid {
		t.Fatal("a pickable colour left data-invalid on the shell")
	}
}

// A conditional region hides with its controls disabled, shows with
// exactly those re-enabled, and a control the page disabled itself is
// left alone throughout.
func TestE2E_WhenHidesDisablesAndRestores(t *testing.T) {
	region := ConditionalField(ConditionalFieldProps{When: "mode", Value: "custom"}, nil,
		Input(InputProps{Name: "detail", ID: "detail"}, nil),
		`<button type="button" id="pageoff" disabled>stays off</button>`,
	)
	sel := Select(SelectProps{Name: "mode", ID: "mode", Options: []Option{
		{Value: "auto", Label: "Auto"}, {Value: "custom", Label: "Custom"},
	}, Selected: "auto"}, nil)
	b := startBehaviorServer(t, string(Form(FormProps{Action: "/x"}, nil, sel, region)))
	ctx := behaviorPage(t, b)

	var state struct {
		Hidden    bool `json:"hidden"`
		InputOff  bool `json:"inputOff"`
		InputMark bool `json:"inputMark"`
		PageOff   bool `json:"pageOff"`
		PageMark  bool `json:"pageMark"`
	}
	probe := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const region = document.querySelector('[data-hui-when]');
			const input = document.getElementById('detail');
			const page = document.getElementById('pageoff');
			return {
				hidden: region.hidden,
				inputOff: input.disabled,
				inputMark: input.hasAttribute('data-hui-when-off'),
				pageOff: page.disabled,
				pageMark: page.hasAttribute('data-hui-when-off'),
			};
		})()`, &state)); err != nil {
			t.Fatalf("reading the region: %v", err)
		}
	}

	// The select ships on "auto", so the region must arrive hidden by
	// the arrival pass.
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the region was never hidden for the unwatched value")
	}
	probe()
	if !state.Hidden || !state.InputOff || !state.InputMark {
		t.Fatalf("hidden: region.hidden=%v input.disabled=%v marked=%v, want hidden, disabled and marked", state.Hidden, state.InputOff, state.InputMark)
	}
	if !state.PageOff || state.PageMark {
		t.Fatalf("the page's own disabled button was touched: disabled=%v marked=%v, want disabled and unmarked", state.PageOff, state.PageMark)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const sel = document.querySelector('[name=mode]');
		sel.value = 'custom';
		sel.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatalf("changing the watched select: %v", err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the region was never shown for the watched value")
	}
	probe()
	if state.Hidden || state.InputOff || state.InputMark {
		t.Fatalf("shown: region.hidden=%v input.disabled=%v marked=%v, want shown, enabled and unmarked", state.Hidden, state.InputOff, state.InputMark)
	}
	if !state.PageOff || state.PageMark {
		t.Fatalf("showing the region touched the page's disabled button: disabled=%v marked=%v", state.PageOff, state.PageMark)
	}
}

// A failed submit moves focus to the summary once per form element: a
// swap that brings a NEW form with the same errors focuses its summary
// (the reader submitted again), and a second scan over the same
// element does not steal focus back after the reader tabbed away.
func TestE2E_FormErrorsFocusTheSummaryOnce(t *testing.T) {
	summary := ValidationSummary(ValidationSummaryProps{
		ID: "f-errors", Errors: []FieldError{{For: "f-name", Message: "Name is required."}},
	}, nil)
	form := Form(FormProps{Action: "/x", Errors: summary}, nil,
		Input(InputProps{Name: "name", ID: "f-name"}, nil))
	// A second form with errors on the same page: only the first
	// summary is focused, and the second is marked in the same pass,
	// so a later scan does not hand it the focus from wherever the
	// reader has moved to.
	second := Form(FormProps{Action: "/y", ID: "second", Errors: ValidationSummary(ValidationSummaryProps{
		ID: "g-errors", Errors: []FieldError{{For: "g-name", Message: "Name is required."}},
	}, nil)}, nil, Input(InputProps{Name: "name", ID: "g-name"}, nil))
	b := startBehaviorServer(t, `<div id="host">`+string(form)+`</div>`+string(second))
	ctx := behaviorPage(t, b)
	const focused = `document.activeElement === document.querySelector('[role="alert"][tabindex="-1"]')`
	if !pollTrue(ctx, focused) {
		t.Fatal("the summary never received focus after load")
	}
	var later string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		document.activeElement.blur();
		window.__gofastr._moduleScanners.headless(document);
		return document.activeElement.tagName;
	})()`, &later)); err != nil {
		t.Fatalf("rescanning with a second form: %v", err)
	}
	if later != "BODY" {
		t.Fatalf("a later scan moved focus to %s: the second form was left unmarked by the first pass", later)
	}

	// Replace the form: same errors, new element. Focus must move to
	// the new summary exactly once.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const host = document.getElementById('host');
		host.innerHTML = host.innerHTML;
	})()`, nil)); err != nil {
		t.Fatalf("swapping the form: %v", err)
	}
	if !pollTrue(ctx, focused) {
		t.Fatal("a new form with the same errors never focused its summary")
	}

	// Blur, then hand the same document to the scanner the way the
	// kernel does after a navigation: the once-guard must hold.
	var active string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		document.activeElement.blur();
		window.__gofastr._moduleScanners.headless(document);
		return document.activeElement.tagName;
	})()`, &active)); err != nil {
		t.Fatalf("rescanning: %v", err)
	}
	if active != "BODY" {
		t.Fatalf("a second scan over the same form moved focus to %s; the once-guard failed", active)
	}
}

// Choosing files lists their names and says the sentence with the
// count and the joined names, from the words the component rendered.
func TestE2E_DropListsFilesAndSaysHowMany(t *testing.T) {
	b := startBehaviorServer(t, string(FileUpload(FileUploadProps{
		Name: "backup", ID: "backup", Multiple: true,
		Label: "Drag archives here, or ", CTA: "choose files",
		Hint: ".tar.gz up to 2 GB", Accept: ".tar.gz",
	}, nil))+string(FileUpload(FileUploadProps{
		Name: "single", ID: "single",
		Label: "Drag one archive here, or ", CTA: "choose a file",
	}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}

	var state struct {
		Items  []string `json:"items"`
		Status string   `json:"status"`
		Count  int      `json:"count"`
	}
	probe := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const root = document.querySelector('[data-hui-drop]');
			return {
				items: [...root.querySelectorAll('[data-hui-drop-list] li')].map((li) => li.textContent),
				status: root.querySelector('[data-hui-drop-status]').textContent,
				count: document.getElementById('backup').files.length,
			};
		})()`, &state)); err != nil {
			t.Fatalf("reading the drop zone: %v", err)
		}
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const dt = new DataTransfer();
		dt.items.add(new File(['a'], 'one.txt', {type: 'text/plain'}));
		dt.items.add(new File(['b'], 'two.txt', {type: 'text/plain'}));
		const input = document.getElementById('backup');
		input.files = dt.files;
		input.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatalf("choosing two files: %v", err)
	}
	probe()
	if len(state.Items) != 2 || state.Items[0] != "one.txt" || state.Items[1] != "two.txt" {
		t.Fatalf("two files listed %v, want one.txt and two.txt", state.Items)
	}
	if want := "2 files selected: one.txt, two.txt."; state.Status != want {
		t.Fatalf("the many sentence was %q, want %q", state.Status, want)
	}

	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const dt = new DataTransfer();
		dt.items.add(new File(['a'], 'solo.csv', {type: 'text/csv'}));
		const input = document.getElementById('backup');
		input.files = dt.files;
		input.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatalf("choosing one file: %v", err)
	}
	probe()
	if len(state.Items) != 1 || state.Items[0] != "solo.csv" {
		t.Fatalf("one file listed %v, want solo.csv", state.Items)
	}
	if want := "solo.csv selected."; state.Status != want {
		t.Fatalf("the one sentence was %q, want %q", state.Status, want)
	}

	// A drop on the zone assigns the files and repaints the list, the
	// path a reader who never opens the picker takes.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const dt = new DataTransfer();
		dt.items.add(new File(['x'], 'dropped.md', {type: 'text/markdown'}));
		dt.items.add(new File(['y'], 'second.md', {type: 'text/markdown'}));
		document.querySelector('[data-hui-drop]')
			.dispatchEvent(new DragEvent('drop', {bubbles: true, dataTransfer: dt}));
	})()`, nil)); err != nil {
		t.Fatalf("dropping on the zone: %v", err)
	}
	probe()
	if state.Count != 2 {
		t.Fatalf("the drop left %d files on the input, want 2", state.Count)
	}
	if len(state.Items) != 2 || state.Items[0] != "dropped.md" || state.Items[1] != "second.md" {
		t.Fatalf("the drop listed %v, want dropped.md and second.md", state.Items)
	}
	if want := "2 files selected: dropped.md, second.md."; state.Status != want {
		t.Fatalf("after the drop the sentence was %q, want %q", state.Status, want)
	}
	// A zone whose input takes one file keeps one from a drop of two,
	// the rule the picker already applies.
	var single struct {
		Count  int    `json:"count"`
		Status string `json:"status"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const dt = new DataTransfer();
		dt.items.add(new File(['x'], 'first.md', {type: 'text/markdown'}));
		dt.items.add(new File(['y'], 'extra.md', {type: 'text/markdown'}));
		const root = document.querySelector('[data-hui-drop-input="single"]');
		root.dispatchEvent(new DragEvent('drop', {bubbles: true, dataTransfer: dt}));
		return {
			count: document.getElementById('single').files.length,
			status: root.querySelector('[data-hui-drop-status]').textContent,
		};
	})()`, &single)); err != nil {
		t.Fatalf("dropping two files on a single-file zone: %v", err)
	}
	if single.Count != 1 || single.Status != "first.md selected." {
		t.Fatalf("a single-file zone took %d files and said %q, want one file and its sentence", single.Count, single.Status)
	}
}

// A rolled-back optimistic mutation announces its failure sentence in
// the polite status span, the words coming from the root's
// data-hui-action-failed. The 422 is a real response from the test
// server: the primitive's fetch, rollback and event all run, and the
// announcement is what a reader hears of the whole chain.
func TestE2E_ActionFailureIsAnnounced(t *testing.T) {
	b := startBehaviorServer(t, string(OptimisticAction(OptimisticActionProps{
		Endpoint: "/__hui/fail", IdleLabel: "Follow", SuccessLabel: "Following",
	}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-hui-action]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("clicking the action: %v", err)
	}
	const said = `document.querySelector('[data-hui-action-status]').textContent === document.querySelector('[data-hui-action]').getAttribute('data-hui-action-failed')`
	if !pollTrue(ctx, said) {
		t.Fatal("the status span never said the failure sentence from the root")
	}
	var sentence, state string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-action-status]').textContent`, &sentence),
		chromedp.Evaluate(`document.querySelector('[data-hui-action]').getAttribute('data-state')`, &state),
	); err != nil {
		t.Fatalf("reading the outcome: %v", err)
	}
	if sentence != "Could not save. Try again." {
		t.Fatalf("the failure sentence was %q, want the Words default", sentence)
	}
	// The rollback passes through error and returns to idle, so the
	// button can be tried again.
	if !pollTrue(ctx, `document.querySelector('[data-hui-action]').getAttribute('data-state') === 'idle'`) {
		t.Fatalf("the rolled-back button stayed in %q", state)
	}
}

// A failed toggle announces too: the primitive dispatches
// action:rolled-back for a failed commit on either button, where the
// old toggle module reverted in silence.
func TestE2E_ToggleFailureIsAnnounced(t *testing.T) {
	b := startBehaviorServer(t, string(ToggleAction(ToggleActionProps{
		Endpoint: "/__hui/fail", IdleLabel: "Watch", CommittedLabel: "Watching",
	}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-hui-action]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("clicking the toggle: %v", err)
	}
	const said = `document.querySelector('[data-hui-action-status]').textContent === document.querySelector('[data-hui-action]').getAttribute('data-hui-action-failed')`
	if !pollTrue(ctx, said) {
		t.Fatal("a failed toggle never announced its failure sentence")
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-action]').getAttribute('data-state') === 'idle'`) {
		t.Fatal("the failed toggle never returned to idle")
	}
}

// An action button that arrives after load is bound by the kernel's
// insertion scan, the arrival the module's registered scanner exists
// for: replace the region through innerHTML, click the new button,
// and it commits against the real endpoint.
func TestE2E_ActionRebindsAfterSwap(t *testing.T) {
	b := startBehaviorServer(t, `<div id="region">`+string(OptimisticAction(OptimisticActionProps{
		Endpoint: "/__hui/ok", IdleLabel: "Follow", SuccessLabel: "Following",
	}, nil))+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.getElementById('region').innerHTML = `+"`"+string(OptimisticAction(OptimisticActionProps{
			Endpoint: "/__hui/ok", IdleLabel: "Join", SuccessLabel: "Joined",
		}, nil))+"`", nil),
	); err != nil {
		t.Fatalf("swapping the region: %v", err)
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`[data-hui-action]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("clicking the new button: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-action]').getAttribute('data-state') === 'committed'`) {
		t.Fatal("the swapped-in action button never committed")
	}
	var label string
	if err := chromedp.Run(ctx,
		chromedp.Evaluate(`document.querySelector('[data-hui-action-done]').textContent`, &label),
	); err != nil {
		t.Fatalf("reading the committed label: %v", err)
	}
	if label != "Joined" {
		t.Fatalf("the committed label was %q, want the swapped-in button's own", label)
	}
}

// Dismissing a system banner remembers it for the session, and the
// same message arriving again (an island swap) is hidden on arrival.
func TestE2E_SystemDismissIsRemembered(t *testing.T) {
	banner := SystemBanner(SystemBannerProps{
		ID: "sys-e2e", Title: "A new version is ready", Shown: true,
	}, nil)
	b := startBehaviorServer(t, `<div id="host">`+string(banner)+`</div>`)
	ctx := behaviorPage(t, b)
	// The dismiss handler exists only once the module has evaluated; a
	// click before that lands on nothing.
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}

	if err := chromedp.Run(ctx, chromedp.Click(`[data-hui-system-dismiss]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("clicking dismiss: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-system]').hidden`) {
		t.Fatal("the banner was never hidden by its dismiss")
	}
	var stored string
	if err := chromedp.Run(ctx, chromedp.Evaluate(
		`sessionStorage.getItem('gofastr.headless.system.dismissed')`, &stored)); err != nil {
		t.Fatalf("reading the session store: %v", err)
	}
	if !strings.Contains(stored, "sys-e2e") {
		t.Fatalf("the dismissed set was %q, want it to hold sys-e2e", stored)
	}

	// The same banner, shown again by a swap of the same markup: it
	// must arrive hidden.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const host = document.getElementById('host');
		host.innerHTML = host.innerHTML;
	})()`, nil)); err != nil {
		t.Fatalf("swapping the banner back in: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-system]').hidden`) {
		t.Fatal("a dismissed message shown again was not hidden on arrival")
	}
}

// The offline banner follows the connection the framework reports:
// hidden before any retry is scheduled, shown once one is, hidden
// again on reconnect.
func TestE2E_OfflineBannerFollowsTheConnection(t *testing.T) {
	no := false
	b := startBehaviorServer(t, string(SystemBanner(SystemBannerProps{
		ID: "sys-off", Tone: "warning", Offline: true, Dismiss: &no, Title: "Connection lost",
	}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}

	var hidden []bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const banner = document.querySelector('[data-hui-system-offline]');
		const hidden = [];
		const fire = (connected, retryCount) => {
			document.dispatchEvent(new CustomEvent('gofastr:sse-status', {detail: {connected, retryCount}}));
			hidden.push(banner.hidden);
		};
		fire(false, 0);
		fire(false, 1);
		fire(true, 1);
		return hidden;
	})()`, &hidden)); err != nil {
		t.Fatalf("driving the connection: %v", err)
	}
	if len(hidden) != 3 || !hidden[0] || hidden[1] || !hidden[2] {
		t.Fatalf("the offline banner's states were %v, want hidden, shown, hidden", hidden)
	}
}

// A region outside every form watches a name two forms carry. It
// must follow the FIRST control in document order deterministically —
// and, more to the point, it must resync at all: the old listener
// scoped the sync to the changed control's form, so a region no form
// owns never resynced after boot and sat frozen on its initial
// branch, its controls wrongly disabled and their values dropped
// from whichever form the reader did submit.
func TestE2E_WhenOutsideTheFormsFollowsTheFirstControl(t *testing.T) {
	plan := func(id, sel string) render.HTML {
		return Select(SelectProps{Name: "plan", ID: id, Options: []Option{
			{Value: "auto", Label: "Auto"}, {Value: "pro", Label: "Pro"},
		}, Selected: sel}, nil)
	}
	region := ConditionalField(ConditionalFieldProps{When: "plan", Value: "pro"}, nil,
		Input(InputProps{Name: "detail", ID: "outside-detail"}, nil))
	b := startBehaviorServer(t,
		`<form id="fa" action="/a">`+string(plan("plan-a", "auto"))+`</form>`+
			`<form id="fb" action="/b">`+string(plan("plan-b", "auto"))+`</form>`+
			string(region))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the region was never hidden for the unwatched value")
	}

	set := func(id, val string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const sel = document.getElementById('`+id+`');
			sel.value = '`+val+`';
			sel.dispatchEvent(new Event('change', {bubbles: true}));
		})()`, nil)); err != nil {
			t.Fatalf("changing %s: %v", id, err)
		}
	}
	set("plan-b", "pro")
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the second form's control moved a region that follows the first")
	}
	set("plan-a", "pro")
	if !pollTrue(ctx, `!document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("changing the first control in document order never showed the region — it is frozen out of every form's sync")
	}
	var disabled bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('outside-detail').disabled`, &disabled)); err != nil {
		t.Fatalf("reading the control: %v", err)
	}
	if disabled {
		t.Fatal("a shown region left its control disabled, so its value would drop from the submit")
	}
}

// A region inside a form may watch a control no form owns: its own
// form is looked in first, and when it holds no control of that name
// the document is, preferring the form-less one. The old scope read
// the form alone, so the region was hidden whatever the switch said.
func TestE2E_WhenWatchesAControlOutsideItsForm(t *testing.T) {
	mode := Select(SelectProps{Name: "mode", ID: "free-mode", Options: []Option{
		{Value: "basic", Label: "Basic"}, {Value: "custom", Label: "Custom"},
	}, Selected: "basic"}, nil)
	region := ConditionalField(ConditionalFieldProps{When: "mode", Value: "custom"}, nil,
		Input(InputProps{Name: "detail", ID: "in-form-detail"}, nil))
	b := startBehaviorServer(t, string(mode)+
		`<form action="/x">`+string(region)+`</form>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("the region was never hidden for the unwatched value")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const sel = document.getElementById('free-mode');
		sel.value = 'custom';
		sel.dispatchEvent(new Event('change', {bubbles: true}));
	})()`, nil)); err != nil {
		t.Fatalf("changing the form-less switch: %v", err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-when]').hidden`) {
		t.Fatal("a region inside a form never followed the form-less control it watches")
	}
}

// Regions nest, and a region inside a hidden region is out whatever
// its own condition says. The old sync gave every region the one
// re-enable mark, so an inner region whose condition held re-enabled
// the controls the outer region's hiding had disabled — reaching
// past the outer condition both on screen and into the submit.
func TestE2E_NestedWhenRegionsBuryTheInnerOne(t *testing.T) {
	mode := Select(SelectProps{Name: "mode", ID: "mode", Options: []Option{
		{Value: "auto", Label: "Auto"}, {Value: "custom", Label: "Custom"},
	}, Selected: "auto"}, nil)
	inner := ConditionalField(ConditionalFieldProps{When: "level", Value: "extra"}, nil,
		Select(SelectProps{Name: "level", ID: "level", Options: []Option{
			{Value: "basic", Label: "Basic"}, {Value: "extra", Label: "Extra"},
		}, Selected: "extra"}, nil),
		Input(InputProps{Name: "detail", ID: "nested-detail"}, nil))
	outer := ConditionalField(ConditionalFieldProps{When: "mode", Value: "custom"}, nil,
		Input(InputProps{Name: "outer", ID: "outer-note"}, nil),
		inner)
	b := startBehaviorServer(t, string(Form(FormProps{Action: "/x"}, nil, mode, outer)))
	ctx := behaviorPage(t, b)

	var state struct {
		Outer  bool `json:"outer"`
		Inner  bool `json:"inner"`
		Detail bool `json:"detail"`
	}
	probe := func() {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const regions = document.querySelectorAll('[data-hui-when]');
			return {
				outer: regions[0].hidden,
				inner: regions[1].hidden,
				detail: document.getElementById('nested-detail').disabled,
			};
		})()`, &state)); err != nil {
			t.Fatalf("reading the nest: %v", err)
		}
	}
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-when]').length === 2 && document.querySelectorAll('[data-hui-when]')[0].hidden`) {
		t.Fatal("the outer region was never hidden for the unwatched value")
	}
	probe()
	if !state.Outer || !state.Inner || !state.Detail {
		t.Fatalf("boot: outer.hidden=%v inner.hidden=%v detail.disabled=%v, want all hidden/disabled while the outer condition fails and the inner one holds", state.Outer, state.Inner, state.Detail)
	}

	set := func(id, val string) {
		t.Helper()
		if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
			const sel = document.getElementById('`+id+`');
			sel.value = '`+val+`';
			sel.dispatchEvent(new Event('change', {bubbles: true}));
		})()`, nil)); err != nil {
			t.Fatalf("changing %s: %v", id, err)
		}
	}
	set("mode", "custom")
	if !pollTrue(ctx, `!document.querySelectorAll('[data-hui-when]')[0].hidden && !document.querySelectorAll('[data-hui-when]')[1].hidden`) {
		t.Fatal("showing the outer region did not let the inner one obey its own held condition")
	}
	probe()
	if state.Detail {
		t.Fatal("the inner region's control stayed disabled once both conditions held")
	}

	// The inner one hides on its own condition while the outer shows.
	set("level", "basic")
	if !pollTrue(ctx, `document.querySelectorAll('[data-hui-when]')[1].hidden`) {
		t.Fatal("the inner region never hid on its own failed condition")
	}
	probe()
	if !state.Inner || !state.Detail {
		t.Fatalf("inner hidden=%v detail.disabled=%v, want the inner region hidden and its control disabled", state.Inner, state.Detail)
	}
	if state.Outer {
		t.Fatal("the inner condition reached up and hid the outer region")
	}
}

// A control inserted alone inside a hidden region — no region of its
// own in the inserted subtree — is disabled by the arrival pass, the
// way the siblings it joined already were.
func TestE2E_AControlInsertedIntoAHiddenRegionIsDisabled(t *testing.T) {
	sel := Select(SelectProps{Name: "mode", ID: "mode2", Options: []Option{
		{Value: "auto", Label: "Auto"}, {Value: "custom", Label: "Custom"},
	}, Selected: "auto"}, nil)
	region := ConditionalField(ConditionalFieldProps{When: "mode", Value: "custom", ID: "hideout"}, nil)
	b := startBehaviorServer(t, string(Form(FormProps{Action: "/x"}, nil, sel, region)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `document.getElementById('hideout').hidden`) {
		t.Fatal("the region was never hidden for the unwatched value")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('hideout')
		.insertAdjacentHTML('beforeend', '<input name="late" id="late-in-hidden">')`, nil)); err != nil {
		t.Fatalf("inserting into the hidden region: %v", err)
	}
	const disabled = `document.getElementById('late-in-hidden').disabled`
	if !pollTrue(ctx, disabled) {
		t.Fatal("a control inserted into a hidden region stayed enabled: its value would submit from a region the condition had closed")
	}
}

// The dismissed set never applies to the offline banner: it is the
// runtime's, shown when the connection is lost and hidden on
// reconnect, and a dismissal remembered for the session would hide
// the next outage. The store is seeded before the module evaluates
// (a reload), the connection is already lost when the banner is
// re-rendered by an island swap, and the banner must still show.
func TestE2E_OfflineBannerIgnoresTheDismissedSet(t *testing.T) {
	no := false
	banner := SystemBanner(SystemBannerProps{
		ID: "sys-off2", Tone: "warning", Offline: true, Dismiss: &no, Title: "Connection lost",
	}, nil)
	b := startBehaviorServer(t, `<div id="host">`+string(banner)+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		sessionStorage.setItem('gofastr.headless.system.dismissed', '["sys-off2"]');
		window.__gofastr.sseStatus = {connected: false, lastEventAt: 1, retryCount: 2};
		// A sentinel the reload wipes. Polling for the module alone
		// races the navigation: the first poll can answer on the OLD
		// document, where the module is still loaded, and the evaluate
		// after it then lands on the fresh one before the kernel has
		// booted — window.__gofastr undefined, under CI load only.
		window.__preReload = true;
		location.reload();
	})()`, nil)); err != nil {
		t.Fatalf("seeding the dismissed set and reloading: %v", err)
	}
	if !pollTrue(ctx, `!window.__preReload && `+moduleLoadedExpr) {
		t.Fatal("the module never loaded after the reload")
	}
	// The reload made a fresh document, so the mirror sse.js owns is
	// set again here, exactly as sse.js would have it mid-outage. The
	// connection is lost with a retry scheduled: the event shows the
	// banner, and an island swap that renders it again — even shown,
	// which a hostile render could ship — must not let the stale
	// dismissal hide it.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__gofastr.sseStatus = {connected: false, lastEventAt: 1, retryCount: 2};
		document.dispatchEvent(new CustomEvent('gofastr:sse-status',
			{detail: window.__gofastr.sseStatus}));
	})()`, nil)); err != nil {
		t.Fatalf("driving the connection: %v", err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-system-offline]').hidden`) {
		t.Fatal("the offline banner never showed for a lost connection")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const host = document.getElementById('host');
		host.innerHTML = host.innerHTML.replace(' hidden=""', '');
	})()`, nil)); err != nil {
		t.Fatalf("re-rendering the banner shown: %v", err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-system-offline]').hidden`) {
		t.Fatal("a dismissed id in the session store hid the runtime's own banner")
	}
}

// A banner that arrives after the connection was already lost reads
// the state sse.js mirrors onto window.__gofastr.sseStatus instead of
// waiting for the next event: an island swap during an outage must
// show the banner at once, not at the next blip.
func TestE2E_OfflineBannerReadsTheConnectionItMissed(t *testing.T) {
	no := false
	banner := SystemBanner(SystemBannerProps{
		ID: "sys-off3", Tone: "warning", Offline: true, Dismiss: &no, Title: "Connection lost",
	}, nil)
	b := startBehaviorServer(t, `<div id="host">`+string(banner)+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__gofastr.sseStatus = {connected: false, lastEventAt: 1, retryCount: 1};
		const host = document.getElementById('host');
		host.innerHTML = host.innerHTML;
	})()`, nil)); err != nil {
		t.Fatalf("losing the link and re-rendering the banner: %v", err)
	}
	if !pollTrue(ctx, `!document.querySelector('[data-hui-system-offline]').hidden`) {
		t.Fatal("a banner that arrived during an outage waited for the next event instead of reading the mirrored state")
	}
	// The mirrored state is re-read on every arrival: a swap while
	// reconnected keeps it hidden.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		window.__gofastr.sseStatus.connected = true;
		const host = document.getElementById('host');
		host.innerHTML = host.innerHTML;
	})()`, nil)); err != nil {
		t.Fatalf("reconnecting and re-rendering: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('[data-hui-system-offline]').hidden`) {
		t.Fatal("a banner that arrived while reconnected showed anyway")
	}
}

// The once-mark is spent only when a summary was found: a form whose
// Errors is not a summary keeps its mark, so the summary that arrives
// on a later render of the same form element still takes focus. The
// old code marked the form on the first pass and the failed submit
// stayed unannounced forever.
func TestE2E_FormErrorsFocusWhenTheSummaryArrivesLater(t *testing.T) {
	form := Form(FormProps{Action: "/x", Errors: render.HTML(`<p id="plain">Check the fields.</p>`)}, nil,
		Input(InputProps{Name: "name", ID: "later-name"}, nil))
	b := startBehaviorServer(t, `<div id="host">`+string(form)+`</div>`)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	const focused = `document.activeElement === document.querySelector('#host [role="alert"][tabindex="-1"]')`
	var moved bool
	if err := chromedp.Run(ctx, chromedp.Evaluate(focused, &moved)); err != nil {
		t.Fatalf("reading focus: %v", err)
	}
	if moved {
		t.Fatal("a form with no summary moved focus to something")
	}
	summary := ValidationSummary(ValidationSummaryProps{
		ID: "later-errors", Errors: []FieldError{{For: "later-name", Message: "Name is required."}},
	}, nil)
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.getElementById('plain')
		.outerHTML = `+"`"+string(summary)+"`", nil)); err != nil {
		t.Fatalf("swapping the plain message for a summary: %v", err)
	}
	// The kernel's post-navigation pass hands the whole document to
	// the scanner; the swap itself may carry no form element at all.
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.__gofastr._moduleScanners.headless(document)`, nil)); err != nil {
		t.Fatalf("rescanning: %v", err)
	}
	if !pollTrue(ctx, focused) {
		t.Fatal("the summary that arrived after a plain Errors node never received focus — the once-mark was spent on nothing")
	}
}

// The drop zone resolves its input on every event, so a swap that
// replaced the input while the zone survived still lands the drop on
// the control the form submits. The old capture-at-arm-time left the
// listeners writing to a detached element.
func TestE2E_DropFollowsAReplacedInput(t *testing.T) {
	b := startBehaviorServer(t, string(FileUpload(FileUploadProps{
		Name: "backup2", ID: "backup2", Multiple: true,
		Label: "Drag archives here, or ", CTA: "choose files",
	}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const old = document.getElementById('backup2');
		const fresh = old.cloneNode(false);
		old.replaceWith(fresh);
	})()`, nil)); err != nil {
		t.Fatalf("replacing the input: %v", err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const dt = new DataTransfer();
		dt.items.add(new File(['x'], 'after-swap.md', {type: 'text/markdown'}));
		document.querySelector('[data-hui-drop]')
			.dispatchEvent(new DragEvent('drop', {bubbles: true, dataTransfer: dt}));
	})()`, nil)); err != nil {
		t.Fatalf("dropping after the swap: %v", err)
	}
	var state struct {
		Count int    `json:"count"`
		First string `json:"first"`
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const root = document.querySelector('[data-hui-drop]');
		return {
			count: document.getElementById('backup2').files.length,
			first: root.querySelector('[data-hui-drop-list] li').textContent,
		};
	})()`, &state)); err != nil {
		t.Fatalf("reading the drop zone: %v", err)
	}
	if state.Count != 1 || state.First != "after-swap.md" {
		t.Fatalf("after the swap the input holds %d files and the list says %q, want one after-swap.md on the control the form submits", state.Count, state.First)
	}
}

// The island form's error convention, end to end: a failed validation
// is answered 200 with the region's HTML — the errors ARE the answer
// — the runtime swaps the region, and the arrival pass focuses the
// summary. A non-2xx would land in the signal as {ok:false, status,
// text} and render nothing, which is why the convention is 200.
func TestE2E_IslandFormFailureFocusesTheSummary(t *testing.T) {
	failed := Form(FormProps{
		Action: "/x", Island: Island{Endpoint: "/__hui/form", Signal: "acct"},
		Errors: ValidationSummary(ValidationSummaryProps{
			ID: "acct-errors", Errors: []FieldError{{For: "acct-name", Message: "Name is required."}},
		}, nil),
	}, nil, Input(InputProps{Name: "name", ID: "acct-name"}, nil),
		Button(ButtonProps{Label: "Save", Type: "submit", Variant: "primary"}, nil))
	b := startBehaviorServer(t,
		`<div id="isle" data-fui-signal="acct" data-fui-signal-mode="html">`+string(failed)+`</div>`,
		func(mux *http.ServeMux) {
			mux.HandleFunc("/__hui/form", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, string(failed))
			})
		})
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	if err := chromedp.Run(ctx,
		chromedp.Click(`#isle button[type="submit"]`, chromedp.ByQuery),
	); err != nil {
		t.Fatalf("clicking submit: %v", err)
	}
	const focused = `document.activeElement === document.querySelector('#isle [role="alert"][tabindex="-1"]')`
	if !pollTrue(ctx, focused) {
		t.Fatal("the summary in the island's answer never received focus — a failed validation answered 200 must announce itself")
	}
}

// Markup that arrives after load is bound: the kernel's insertion scan
// loads the module for a late marker, and the behaviours work on it
// with nothing re-armed by hand.
func TestE2E_LateMarkupIsBound(t *testing.T) {
	b := startBehaviorServer(t, string(Badge(BadgeProps{Label: "running"}, nil)))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, bootSettledExpr) {
		t.Fatal("the page never finished booting")
	}
	if n := b.hits.Load(); n != 0 {
		t.Fatalf("a badge-only page fetched headless.js %d times before any insertion", n)
	}

	late, err := json.Marshal(string(Password(PasswordProps{Name: "late", ID: "late"}, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`document.querySelector('main')
		.insertAdjacentHTML('beforeend', '<div id="late">'+`+string(late)+`+'</div>')`, nil)); err != nil {
		t.Fatalf("inserting a password late: %v", err)
	}
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the late marker never loaded the module through the insertion scan")
	}
	if n := b.hits.Load(); n != 1 {
		t.Fatalf("headless.js fetched %d times for the late marker, want 1", n)
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#late [data-hui-reveal]`, chromedp.ByQuery)); err != nil {
		t.Fatalf("clicking the late reveal: %v", err)
	}
	if !pollTrue(ctx, `document.querySelector('#late [data-hui-affix-input]').type === 'text'`) {
		t.Fatal("the late reveal button did nothing: the inserted markup was never bound")
	}
}

// A swap inside a region can be what restores the region's gating
// value: an island re-renders the radios the region watches, with the
// showing value selected, together with a nested region whose own
// condition holds. The arrival pass must sync the enclosing region
// BEFORE the regions inside the swap, or the inner one reads the
// enclosing region's stale hidden state and stays buried, with its
// controls disabled, until the user touches some other control.
func TestE2E_ASwapThatRestoresTheGatingValueUnburiesTheInnerRegion(t *testing.T) {
	inner := `<div data-hui-when="tier" data-hui-when-value="pro" id="inner">` +
		`<input type="checkbox" name="tier" value="pro" id="tier" checked>` +
		`<input name="seat" id="seat"></div>`
	radios := func(mode string) string {
		checked := map[string]string{"auto": "", "custom": ""}
		checked[mode] = " checked"
		return `<div id="swap"><input type="radio" name="mode" value="auto" id="m-auto"` + checked["auto"] + `>` +
			`<input type="radio" name="mode" value="custom" id="m-custom"` + checked["custom"] + `>` + inner + `</div>`
	}
	outer := `<div data-hui-when="mode" data-hui-when-value="custom" id="outer">` + radios("auto") + `</div>`
	b := startBehaviorServer(t, string(Form(FormProps{Action: "/x"}, nil, render.HTML(outer))))
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, `document.getElementById('outer').hidden && document.getElementById('inner').hidden`) {
		t.Fatal("the regions were never buried for the hiding value")
	}
	// The swap: the radios come back with custom selected, and the inner
	// region with them. No input or change event fires, as none does
	// for an island swap; only the arrival pass sees it.
	js := `document.getElementById('swap').outerHTML = ` + "`" + radios("custom") + "`"
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, nil)); err != nil {
		t.Fatalf("swapping the radios: %v", err)
	}
	if !pollTrue(ctx, `!document.getElementById('outer').hidden`) {
		t.Fatal("the enclosing region stayed hidden after the swap restored its gating value")
	}
	if !pollTrue(ctx, `!document.getElementById('inner').hidden && !document.getElementById('seat').disabled`) {
		t.Fatal("the inner region stayed buried: the arrival pass read the enclosing region's stale hidden state")
	}
}

// A region outside every form prefers a control no form owns over a
// same-named control inside a form: the loose one is the page-level
// switch a region outside the forms belongs to, and the form's control
// of that name is that form's business.
func TestE2E_WhenOutsideTheFormsPrefersTheLooseControl(t *testing.T) {
	// The form's select comes FIRST in document order, so a lookup
	// that merely takes the first match follows it; only the
	// preference for controls no form owns reaches the radios below.
	page := `<form action="/x"><select name="plan" id="form-plan"><option value="basic">Basic</option>` +
		`<option value="pro" selected>Pro</option></select></form>` +
		`<input type="radio" name="plan" value="basic" id="loose-basic" checked>` +
		`<input type="radio" name="plan" value="pro" id="loose-pro">` +
		`<div data-hui-when="plan" data-hui-when-value="pro" id="pro-only"><input name="seats" id="seats"></div>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	// The form's select says pro; the loose radios say basic. The
	// region follows the radios.
	if !pollTrue(ctx, `document.getElementById('pro-only').hidden`) {
		t.Fatal("the region followed the form's select instead of the loose radios")
	}
	if err := chromedp.Run(ctx, chromedp.Click(`#loose-pro`, chromedp.ByID)); err != nil {
		t.Fatalf("clicking the loose radio: %v", err)
	}
	if !pollTrue(ctx, `!document.getElementById('pro-only').hidden`) {
		t.Fatal("the region never followed the loose radio")
	}
}

// A form's controls are the ones it owns, not the ones inside it: a
// select outside the form element with form="id" belongs to the form,
// and a region inside that form watching it must follow it, ahead of
// a same-named loose control that stands earlier in the document.
func TestE2E_WhenFollowsAFormAssociatedControlOutsideTheFormElement(t *testing.T) {
	page := `<input type="radio" name="mode" value="custom" id="decoy" checked>` +
		string(Form(FormProps{Action: "/x", ID: "owner"}, nil,
			render.HTML(`<div data-hui-when="mode" data-hui-when-value="custom" id="owned"><input name="seat" id="seat2"></div>`))) +
		`<select name="mode" id="assoc" form="owner"><option value="auto" selected>Auto</option><option value="custom">Custom</option></select>`
	b := startBehaviorServer(t, page)
	ctx := behaviorPage(t, b)
	if !pollTrue(ctx, moduleLoadedExpr) {
		t.Fatal("the module never loaded")
	}
	// The decoy says custom and stands first; the form's own control,
	// attached by form=, says auto. The region follows the form's.
	if !pollTrue(ctx, `document.getElementById('owned').hidden`) {
		t.Fatal("the region followed the loose decoy instead of the control the form owns through form=")
	}
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => { const s = document.getElementById('assoc'); s.value = 'custom'; s.dispatchEvent(new Event('change', {bubbles: true})); })()`, nil)); err != nil {
		t.Fatalf("changing the associated select: %v", err)
	}
	if !pollTrue(ctx, `!document.getElementById('owned').hidden`) {
		t.Fatal("the region never followed the form-associated control")
	}
}
