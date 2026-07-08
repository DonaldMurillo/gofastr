// Package interactive provides declarative interactivity primitives for
// GoFastr components. It wraps arbitrary render.HTML with data-fui-*
// attributes the runtime understands — RPC calls, signal bindings, widget
// chaining — without writing any JavaScript.
//
// Usage:
//
//	interactive.OnClick(btn,
//	    interactive.Post("/api/like").OnSuccess(interactive.SetSignal("count")),
//	)
//
// The package only emits attributes the runtime already handles
// (data-fui-rpc, data-fui-signal, data-fui-open, etc.) plus new ones
// added for chaining (data-fui-rpc-open, data-fui-rpc-signal).
package interactive

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Actions ────────────────────────────────────────────────────────
//
// An Action describes what happens on a user interaction (click or
// submit). It maps 1:1 to data-fui-rpc attributes on the HTML element.

// Action describes an RPC call triggered by click or submit.
type Action struct {
	method  string // GET, POST, PUT, DELETE, PATCH
	path    string // URL path
	confirm string // pre-flight window.confirm message (empty = none)
	effects []Effect
}

// Post creates a POST action. Panics if path does not start with "/".
func Post(path string) Action {
	return newAction("POST", path)
}

// Get creates a GET action. Panics if path does not start with "/".
func Get(path string) Action {
	return newAction("GET", path)
}

// Put creates a PUT action. Panics if path does not start with "/".
func Put(path string) Action {
	return newAction("PUT", path)
}

// Delete creates a DELETE action. Panics if path does not start with "/".
func Delete(path string) Action {
	return newAction("DELETE", path)
}

// Patch creates a PATCH action. Panics if path does not start with "/".
func Patch(path string) Action {
	return newAction("PATCH", path)
}

func newAction(method, path string) Action {
	if !strings.HasPrefix(path, "/") {
		panic(fmt.Sprintf("interactive: path must start with '/', got %q", path))
	}
	return Action{method: method, path: path}
}

// OnSuccess adds effects that run when the RPC returns 2xx.
func (a Action) OnSuccess(effects ...Effect) Action {
	a.effects = append(a.effects, effects...)
	return a
}

// WithConfirm gates the action behind a PRE-FLIGHT confirmation. Before the
// RPC is dispatched, the runtime shows a native window.confirm(message)
// dialog; cancelling aborts the request entirely, so the RPC never fires.
// Because the gate runs *before* the request — not after it succeeds — it is
// a property of the Action itself, not an OnSuccess effect. Use for
// destructive actions (delete, revoke, drop):
//
//	interactive.OnClick(deleteBtn,
//	    interactive.Delete("/api/items/42").
//	        WithConfirm("Delete this item? This cannot be undone."),
//	)
//
// window.confirm is native, unthemed, and blocks browser automation. For a
// design-system-styled confirmation that matches the rest of the app (and is
// drivable by tests), reach for framework/ui.ConfirmAction instead — it
// renders a themed alertdialog whose Confirm button carries the RPC.
//
// Maps to data-fui-confirm="message".
func (a Action) WithConfirm(message string) Action {
	a.confirm = message
	return a
}

// ─── Effects ────────────────────────────────────────────────────────
//
// Effects describe what happens after an RPC succeeds or fails.
// They map to data-fui-rpc-* attributes.

// Effect is something that happens after an RPC response.
type Effect interface {
	// rpcAttrs returns data-fui-rpc-* attributes to set on the element.
	rpcAttrs() map[string]string
}

// SetSignal pushes the RPC response into a named client-side signal.
// Maps to data-fui-rpc-signal="name". Panics if name contains a double quote.
func SetSignal(name string) Effect {
	if strings.ContainsRune(name, '"') {
		panic(fmt.Sprintf("interactive: signal name must not contain '\"', got %q", name))
	}
	return signalEffect{name: name}
}

type signalEffect struct{ name string }

func (e signalEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-signal": e.name}
}

// OpenWidget opens a named widget when the RPC succeeds.
// Maps to data-fui-rpc-open="name".
func OpenWidget(name string) Effect {
	return openWidgetEffect{name: name}
}

type openWidgetEffect struct{ name string }

func (e openWidgetEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-open": e.name}
}

// CloseWidget closes the enclosing widget on RPC success.
// Maps to data-fui-rpc-close="true".
func CloseWidget() Effect {
	return closeWidgetEffect{}
}

type closeWidgetEffect struct{}

func (e closeWidgetEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-close": "true"}
}

// ResetForm resets the enclosing form on RPC success.
// Maps to data-fui-rpc-reset="true".
func ResetForm() Effect {
	return resetFormEffect{}
}

type resetFormEffect struct{}

func (e resetFormEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-reset": "true"}
}

// Navigate does an SPA navigation on RPC success.
// Maps to data-fui-rpc-navigate="path".
func Navigate(path string) Effect {
	return navigateEffect{path: path}
}

type navigateEffect struct{ path string }

func (e navigateEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-navigate": e.path}
}

// Confirm shows a window.confirm dialog before the RPC fires. The RPC is
// cancelled if the user dismisses the dialog.
//
// Deprecated: Confirm is passed to OnSuccess(...) even though it fires
// PRE-flight (before the request, not after success) — a placement that has
// misled readers into thinking the gate runs on the response. Use
// Action.WithConfirm(message) instead, which reads in the correct order and
// lives where the timing implies. This effect still works and maps to the
// same data-fui-confirm attribute.
func Confirm(message string) Effect {
	return confirmEffect{message: message}
}

type confirmEffect struct{ message string }

func (e confirmEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-confirm": e.message}
}

// AfterText replaces the trigger element's text content with text on 2xx RPC
// success. One-shot — subsequent re-clicks are idempotent via
// data-fui-rpc-after-done. Pair with AfterDisable for "Saved ✓" feedback.
// Maps to data-fui-rpc-after-text="text".
func AfterText(text string) Effect {
	return afterTextEffect{text: text}
}

type afterTextEffect struct{ text string }

func (e afterTextEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-after-text": e.text}
}

// AfterDisable permanently disables the trigger element on 2xx RPC success
// (sets aria-disabled="true" and, for buttons/inputs, disabled=true). Use
// with AfterText for "Saved ✓" / "Revealed ✓" feedback. Maps to the boolean
// attribute data-fui-rpc-after-disable.
func AfterDisable() Effect {
	return afterDisableEffect{}
}

type afterDisableEffect struct{}

func (e afterDisableEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-after-disable": ""}
}

// ScrollTo smooth-scrolls the element matching selector into view on 2xx RPC
// success. Use to direct the user's eye at newly-inserted content.
// Maps to data-fui-rpc-scroll-to="selector".
func ScrollTo(selector string) Effect {
	return scrollToEffect{selector: selector}
}

type scrollToEffect struct{ selector string }

func (e scrollToEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-rpc-scroll-to": e.selector}
}

// PushState applies a URL update via history.pushState after 2xx RPC success
// without triggering a fetch. The server-supplied X-Gofastr-Push-State header
// takes precedence over this attribute when both are present.
// Maps to data-fui-push-state="path".
func PushState(path string) Effect {
	return pushStateEffect{path: path}
}

type pushStateEffect struct{ path string }

func (e pushStateEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-fui-push-state": e.path}
}

// ─── Wrapper functions ──────────────────────────────────────────────

// OnClick wraps an HTML element so that clicking it fires the action.
// The element must be a clickable element (button, a, etc.).
func OnClick(html render.HTML, action Action) render.HTML {
	return wrapWithAction(html, action)
}

// OnSubmit wraps a form element so that submitting it fires the action.
func OnSubmit(html render.HTML, action Action) render.HTML {
	return wrapWithAction(html, action)
}

// LiveSearch wraps a form element so input changes fire debounced RPCs.
// The search input should be the first <input> inside the form.
// Results are written into the signal named by the action's OnSuccess effect
// (typically via SetSignal).
//
// debounceMs controls the delay between keystrokes and the RPC call.
// If debounceMs is 0, a default of 300ms is used.
func LiveSearch(form render.HTML, action Action, debounceMs int) render.HTML {
	wrapped := wrapWithAction(form, action)
	wrapped = injectAttr(wrapped, "data-fui-rpc-trigger", "input")
	ms := debounceMs
	if ms == 0 {
		ms = 300
	}
	wrapped = injectAttr(wrapped, "data-fui-rpc-debounce-ms", fmt.Sprintf("%d", ms))
	return wrapped
}

// ─── Scroll-triggered reveal ────────────────────────────────────────

// revealStyle ships the CSS for the scroll-reveal animation. Without it
// the reveal.js classes (fui-hidden / fui-revealed / fui-reveal-<type>)
// have no visual effect. The host loads it when a page carries the
// data-fui-comp="fui-reveal" marker Reveal stamps below.
var revealStyle = registry.RegisterStyle("fui-reveal", revealCSS)

// Reveal wraps an element so it animates in when it enters the viewport.
// The animationType determines the direction ("fade-up", "fade-in",
// "slide-left", "slide-right"). Empty → "fade-in". The element renders
// visible without JS (progressive enhancement); reveal.js adds the
// hidden state on boot and removes it on intersection.
func Reveal(html render.HTML, animationType string) render.HTML {
	if animationType == "" {
		animationType = "fade-in"
	}
	out := injectAttr(html, "data-fui-reveal", animationType)
	return revealStyle.WrapHTML(out)
}

func revealCSS(_ style.Theme) string {
	// While hidden, the direction transform is keyed off the
	// data-fui-reveal ATTRIBUTE (present the whole time) — reveal.js only
	// adds the fui-reveal-<type> CLASS at reveal time, too late to style
	// the from-state. On reveal, fui-hidden is removed and fui-revealed
	// adds the transition back to the resting state.
	return `[data-fui-comp="fui-reveal"]{opacity:1}` +
		`[data-fui-comp="fui-reveal"].fui-hidden{opacity:0}` +
		`[data-fui-comp="fui-reveal"][data-fui-reveal="fade-up"].fui-hidden{transform:translateY(24px)}` +
		`[data-fui-comp="fui-reveal"][data-fui-reveal="slide-left"].fui-hidden{transform:translateX(24px)}` +
		`[data-fui-comp="fui-reveal"][data-fui-reveal="slide-right"].fui-hidden{transform:translateX(-24px)}` +
		`[data-fui-comp="fui-reveal"].fui-revealed{opacity:1;transform:none;transition:opacity .6s ease,transform .6s ease}` +
		`@media (prefers-reduced-motion:reduce){[data-fui-comp="fui-reveal"].fui-hidden{opacity:1;transform:none}[data-fui-comp="fui-reveal"].fui-revealed{transition:none}}`
}

// ─── Client-side signal mutations (no RPC) ──────────────────────────
//
// These mutate signals purely in the browser — no server round-trip.
// Use for counters, toggles, tabs, and other local-only state.

// SetLocal wraps an HTML element so clicking it sets a signal to a
// fixed value. No RPC is fired — the update is instant.
func SetLocal(html render.HTML, signalName, value string) render.HTML {
	return injectAttr(html, "data-fui-signal-set", signalName+":"+value)
}

// IncLocal wraps an HTML element so clicking it increments a numeric
// signal by delta (default 1). No RPC is fired.
func IncLocal(html render.HTML, signalName string, delta int) render.HTML {
	val := signalName
	if delta != 1 {
		val = fmt.Sprintf("%s:%d", signalName, delta)
	}
	return injectAttr(html, "data-fui-signal-inc", val)
}

// ToggleLocal wraps an HTML element so clicking it toggles a boolean
// signal. No RPC is fired.
func ToggleLocal(html render.HTML, signalName string) render.HTML {
	return injectAttr(html, "data-fui-signal-toggle", signalName)
}

// ─── Dropdown ──────────────────────────────────────────────────────
//
// Dropdown wraps a trigger element and a panel into a click-toggle
// dropdown. The trigger gets data-fui-dropdown, aria-expanded="false",
// and aria-haspopup="true". The panel gets data-fui-dropdown-panel and
// is initially hidden. Both are wrapped in a container with
// data-fui-dropdown-wrap.
//
// The runtime module (dropdown.js) handles click-toggle, click-outside
// dismiss, and Escape-to-close.
//
//	trigger := render.Tag("button", nil, render.Text("Menu"))
//	panel := render.Tag("div", nil, render.Text("Dropdown content"))
//	html := interactive.Dropdown(trigger, panel)
var dropdownStyle = registry.RegisterStyle("fui-dropdown", dropdownCSS)

func Dropdown(trigger, panel render.HTML) render.HTML {
	triggerAttrs := map[string]string{
		"data-fui-dropdown": "",
		"aria-expanded":     "false",
		"aria-haspopup":     "true",
	}
	panelAttrs := map[string]string{
		"data-fui-dropdown-panel": "",
		"hidden":                  "",
	}
	wrappedTrigger := injectAttrs(trigger, triggerAttrs)
	wrappedPanel := injectAttrs(panel, panelAttrs)
	wrap := render.Tag("div", map[string]string{
		"data-fui-dropdown-wrap": "",
	}, wrappedTrigger, wrappedPanel)
	return dropdownStyle.WrapHTML(wrap)
}

func dropdownCSS(_ style.Theme) string {
	// The wrap positions the panel; the panel is a floating surface; its
	// links/buttons are styled as menu items. Without this the panel
	// renders as a flat, full-width, unstyled strip (functional but not a
	// dropdown).
	return `[data-fui-comp="fui-dropdown"]{position:relative;display:inline-block}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel]{position:absolute;top:calc(100% + 4px);left:0;min-width:11rem;background:var(--fui-surface,#fff);border:1px solid var(--fui-border,#e2e8f0);border-radius:.5rem;box-shadow:0 8px 24px rgba(0,0,0,.12);padding:var(--spacing-sm, .25rem);z-index:50}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] a,[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] button{display:block;width:100%;box-sizing:border-box;text-align:left;padding:var(--spacing-md, .5rem) .75rem;border-radius:.375rem;color:var(--fui-foreground,#0f172a);text-decoration:none;background:none;border:none;cursor:pointer;font:inherit;font-size:var(--text-sm, .875rem)}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] a:hover,[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] button:hover{background:var(--fui-muted-bg,#f1f5f9)}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] a:focus-visible,[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] button:focus-visible{outline:2px solid var(--fui-primary,#3b82f6);outline-offset:-2px}`
}

// AnimateOnSignal wraps an element so it gets a CSS class when a signal
// is truthy and loses it when falsy. Used for CSS transition-driven
// animations like slide-down, fade, etc.
//
// Panics if signalName or cssClass is empty.
func AnimateOnSignal(html render.HTML, signalName, cssClass string) render.HTML {
	if signalName == "" {
		panic("interactive: AnimateOnSignal signalName must not be empty")
	}
	if cssClass == "" {
		panic("interactive: AnimateOnSignal cssClass must not be empty")
	}
	html = injectAttr(html, "data-fui-animate-signal", signalName)
	html = injectAttr(html, "data-fui-animate-class", cssClass)
	return html
}

// EditToggle wraps an element so clicking it toggles a boolean signal.
// Semantic alias for ToggleLocal used in inline-edit patterns: clicking
// the text span enters edit mode.
func EditToggle(html render.HTML, signalName string) render.HTML {
	return ToggleLocal(html, signalName)
}

// CancelEdit wraps an element so clicking it sets a signal to false,
// closing the inline-edit mode. Typically used on a cancel button
// inside the edit form.
func CancelEdit(html render.HTML, signalName string) render.HTML {
	return SetLocal(html, signalName, "false")
}

// ─── Optimistic update ─────────────────────────────────────────────
//
// OptimisticUpdate renders a button that flips to its "success" visual
// state immediately on click, fires an RPC in the background, and
// reverts to idle if the RPC fails (non-2xx or network error).
//
// The runtime module optimisticaction.js handles the full lifecycle:
// idle → pending (optimistic flip) → committed (RPC 2xx) or error → idle.
//
// The caller provides two visual states:
//   - idle:    the default appearance (e.g. "♡ Like")
//   - success: the committed appearance (e.g. "♥ Liked")
//
// Example:
//
//	OptimisticUpdate(
//	    interactive.Post("/api/like/42"),
//	    render.HTML(`<span class="icon">♡</span> Like`),
//	    render.HTML(`<span class="icon">♥</span> Liked`),
//	)
//
// Produces:
//
//	<button data-fui-comp="ui-optimistic-action"
//	        data-state="idle"
//	        data-fui-optimistic-endpoint="/api/like/42"
//	        data-fui-optimistic-method="POST">
//	  <span data-fui-optimistic-idle><span class="icon">♡</span> Like</span>
//	  <span hidden data-fui-optimistic-success><span class="icon">♥</span> Liked</span>
//	</button>
func OptimisticUpdate(action Action, idle, success render.HTML) render.HTML {
	attrs := map[string]string{
		"data-fui-comp":                "ui-optimistic-action",
		"data-state":                   "idle",
		"data-fui-optimistic-endpoint": action.path,
	}
	if action.method != "" && action.method != "POST" {
		attrs["data-fui-optimistic-method"] = action.method
	}
	idleSpan := render.Tag("span", map[string]string{
		"data-fui-optimistic-idle": "",
	}, idle)
	successSpan := render.Tag("span", map[string]string{
		"data-fui-optimistic-success": "",
		"hidden":                      "",
	}, success)
	return render.Tag("button", attrs, idleSpan, successSpan)
}

// injectAttr adds a single data-fui-* attribute to the first HTML tag.
func injectAttr(html render.HTML, key, value string) render.HTML {
	s := string(html)
	a := render.Attr(key, value)
	if a == "" {
		return html
	}
	idx := findUnquotedClose(s)
	if idx == -1 {
		return html
	}
	return render.HTML(s[:idx] + " " + a + s[idx:])
}

// injectAttrs adds multiple attributes to the first HTML tag.
func injectAttrs(html render.HTML, attrs map[string]string) render.HTML {
	s := string(html)
	idx := findUnquotedClose(s)
	if idx == -1 {
		return html
	}
	var buf strings.Builder
	for k, v := range attrs {
		a := render.Attr(k, v)
		if a == "" {
			continue
		}
		buf.WriteByte(' ')
		buf.WriteString(a)
	}
	if buf.Len() == 0 {
		return html
	}
	return render.HTML(s[:idx] + buf.String() + s[idx:])
}

// wrapWithAction merges action attributes into the first opening HTML tag.
func wrapWithAction(html render.HTML, action Action) render.HTML {
	attrs := action.attrs()
	if len(attrs) == 0 {
		return html
	}

	s := string(html)
	var attrStr strings.Builder
	for k, v := range attrs {
		rendered := render.Attr(k, v)
		if rendered == "" {
			continue // Attr drops unsafe keys
		}
		attrStr.WriteByte(' ')
		attrStr.WriteString(rendered)
	}

	// Inject attributes into the opening tag.
	// Find the first unquoted '>' so that attributes like title="1>2"
	// don't cause an incorrect split.
	idx := findUnquotedClose(s)
	if idx == -1 {
		return html
	}

	return render.HTML(s[:idx] + attrStr.String() + s[idx:])
}

// findUnquotedClose returns the byte index of the first '>' that is not
// inside a single- or double-quoted attribute value. Returns -1 if none.
func findUnquotedClose(s string) int {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '>' {
			return i
		}
	}
	return -1
}

// ─── Internal helpers ───────────────────────────────────────────────

func (a Action) attrs() map[string]string {
	if a.method == "" && a.path == "" {
		return nil
	}
	m := map[string]string{
		"data-fui-rpc":        a.path,
		"data-fui-rpc-method": a.method,
	}
	if a.confirm != "" {
		m["data-fui-confirm"] = a.confirm
	}
	for _, e := range a.effects {
		for k, v := range e.rpcAttrs() {
			m[k] = v
		}
	}
	return m
}
