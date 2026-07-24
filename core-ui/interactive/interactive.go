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
	"bytes"
	"encoding/json"
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
	body    string // static JSON body for non-form RPCs (data-fui-rpc-body); empty = none
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

// WithBody attaches a static JSON body to the action — the payload sent
// for a non-form RPC (a button click that isn't inside a <form>).
// Maps to data-fui-rpc-body="<json>". The runtime sends it verbatim as
// the request body with Content-Type: application/json.
//
// Panics if json is not valid JSON (json.Valid), so a malformed body
// fails loudly at build/render time rather than producing a runtime
// request the server rejects. For form-backed RPCs the runtime
// serializes the form fields itself — do not call WithBody there.
func (a Action) WithBody(body string) Action {
	if !json.Valid([]byte(body)) {
		panic(fmt.Sprintf("interactive: WithBody requires valid JSON, got %q", body))
	}
	a.body = body
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
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel]{position:absolute;top:calc(100% + 4px);left:0;min-width:11rem;background:var(--fui-surface, var(--color-surface, #fff));border:1px solid var(--fui-border, var(--color-border, #e2e8f0));border-radius:.5rem;box-shadow:0 8px 24px rgba(0,0,0,.12);padding:var(--spacing-sm, .25rem);z-index:50}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] a,[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] button{display:block;width:100%;box-sizing:border-box;text-align:left;padding:var(--spacing-md, .5rem) .75rem;border-radius:.375rem;color:var(--fui-foreground, var(--color-text, #0f172a));text-decoration:none;background:none;border:none;cursor:pointer;font:inherit;font-size:var(--text-sm, .875rem)}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] a:hover,[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] button:hover{background:var(--fui-muted-bg, var(--color-surface-soft, #f1f5f9))}` +
		`[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] a:focus-visible,[data-fui-comp="fui-dropdown"] [data-fui-dropdown-panel] button:focus-visible{outline:2px solid var(--fui-primary, var(--color-primary, #3b82f6));outline-offset:-2px}`
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
	// safe-html: the source is already typed HTML and a is emitted by
	// render.Attr, which validates the key and escapes the value.
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
	// safe-html: every fragment in buf came from render.Attr.
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

	// safe-html: every fragment in attrStr came from render.Attr.
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
	if a.body != "" {
		m["data-fui-rpc-body"] = a.body
	}
	for _, e := range a.effects {
		for k, v := range e.rpcAttrs() {
			m[k] = v
		}
	}
	return m
}

// Attrs returns the data-fui-* attributes this Action would inject, as a
// plain map[string]string. It is the same map [OnClick]/[OnSubmit] splice
// into the opening tag — exported so a call site can merge it into an
// existing attribute map (an [render.Tag] attrs map or a ui.*Config
// ExtraAttrs) and let render.Tag's sorted writer place every attribute in
// the same order a hand-written map would. Prefer this over the wrapper
// functions whenever byte-identical attribute ordering matters (the
// wrappers append at the tag's first '>', which can reorder attributes
// relative to a sorted map render).
//
// The returned map is a fresh copy; mutating it does not affect the Action.
// It is assignable directly to html.Attrs (a map[string]string alias) and
// to render.Tag's attrs argument.
func (a Action) Attrs() map[string]string {
	return a.attrs()
}

// ─── Widget open triggers ───────────────────────────────────────────

// OpenOnClick wraps an HTML element so clicking it opens a registered
// widget surface. Maps to data-fui-open="<widget>". This is the
// click-to-open trigger — distinct from [OpenWidget], which opens a
// widget only after a successful RPC (data-fui-rpc-open).
func OpenOnClick(html render.HTML, widget string) render.HTML {
	return injectAttr(html, "data-fui-open", widget)
}

// ─── Toasts ─────────────────────────────────────────────────────────

// Toast is the config for a click-fired toast notification. Zero fields
// are omitted from the emitted JSON, so a Toast{Variant, Title, Body,
// TTLMs} marshals to exactly {"variant":…,"title":…,"body":…,"ttl":…} —
// the shape call sites hand-write. The runtime's toast module
// (core-ui/runtime/src/toasts.js __gofastr.toast) reads these keys:
// variant, title, body, ttl, stack.
type Toast struct {
	Variant string `json:"variant,omitempty"` // "success" | "warning" | "danger" | "info" | "neutral"; defaults to "info"
	Title   string `json:"title,omitempty"`   // required by the runtime — a toast with no title is dropped
	Body    string `json:"body,omitempty"`    // optional supporting copy
	Stack   string `json:"stack,omitempty"`   // named [data-fui-toast-stack] container; empty → the auto stack
	TTLMs   int    `json:"ttl,omitempty"`     // auto-dismiss delay in ms; 0 → persistent (manual dismiss only)
}

// ToastOnClick wraps an HTML element so clicking it fires a toast with
// the given config. Maps to data-fui-toast="<json>". The JSON is
// compact (no whitespace) with HTML escaping disabled, matching what
// call sites hand-write today; render.Attr then escapes the value for
// the attribute context.
func ToastOnClick(html render.HTML, t Toast) render.HTML {
	return injectAttr(html, "data-fui-toast", marshalToast(t))
}

// marshalToast encodes a Toast as compact JSON with HTML escaping off
// (so a body containing '<' round-trips through render.Attr identically
// to a hand-written literal, instead of arriving as \u003c).
func marshalToast(t Toast) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t); err != nil {
		// Toast only holds strings and an int — Encode cannot fail in
		// practice. Surface it loudly rather than emitting nothing.
		panic(fmt.Sprintf("interactive: failed to marshal toast: %v", err))
	}
	return strings.TrimSpace(buf.String())
}

// ─── Pane open/close triggers ───────────────────────────────────────

// validPanes are the side-pane slots a PaneHost renders. The empty
// string is a valid ClosePaneOnClick target (close the topmost pane) but
// never a valid OpenPaneOnClick target.
var validPanes = map[string]bool{"secondary": true, "tertiary": true}

// OpenPaneOnClick wraps an HTML element so clicking it opens the named
// side pane. Maps to data-fui-pane-open="<pane>". Panics unless pane is
// "secondary" or "tertiary".
func OpenPaneOnClick(html render.HTML, pane string) render.HTML {
	if !validPanes[pane] {
		panic(fmt.Sprintf("interactive: OpenPaneOnClick pane must be \"secondary\" or \"tertiary\", got %q", pane))
	}
	return injectAttr(html, "data-fui-pane-open", pane)
}

// ClosePaneOnClick wraps an HTML element so clicking it closes a side
// pane. Maps to data-fui-pane-close="<pane>". A non-empty pane
// ("secondary" or "tertiary") closes that specific pane; an empty pane
// emits data-fui-pane-close="" and closes the topmost open pane. Any
// other value panics.
func ClosePaneOnClick(html render.HTML, pane string) render.HTML {
	if pane != "" && !validPanes[pane] {
		panic(fmt.Sprintf("interactive: ClosePaneOnClick pane must be \"secondary\", \"tertiary\", or \"\" (topmost), got %q", pane))
	}
	return injectAttr(html, "data-fui-pane-close", pane)
}

// ─── Signal display bindings ───────────────────────────────────────
//
// These wrap an island content region so its text/HTML/attribute is
// driven by a named client signal. They inject data-fui-signal plus a
// data-fui-signal-mode. The names mirror core-ui/store's Slice.Bind*
// methods (the typed, seeded-signal counterpart): reach for a store
// Slice when the signal is seeded server-side and read by other typed
// code; reach for these wrappers when you are binding an island's HTML
// region to a signal an RPC writes (typically via [SetSignal]).

// BindHTML wraps an HTML region whose innerHTML is replaced with the
// signal value (the trusted-HTML path). Injects data-fui-signal and
// data-fui-signal-mode="html". Use for an island slot an RPC re-renders
// server-side and returns as a fragment.
func BindHTML(html render.HTML, signal string) render.HTML {
	return bindSignal(html, signal, "html", "")
}

// BindText wraps an HTML element whose textContent tracks a signal
// (HTML-escaped). Injects data-fui-signal and data-fui-signal-mode="text".
// This is the default mode — a bare data-fui-signal with no mode behaves
// identically — but emitting the mode explicitly documents intent.
func BindText(html render.HTML, signal string) render.HTML {
	return bindSignal(html, signal, "text", "")
}

// BindAttr wraps an HTML element whose attribute tracks a signal.
// Injects data-fui-signal, data-fui-signal-mode="attr", and
// data-fui-signal-attr="<attr>". Use when a signal should drive a single
// attribute (e.g. aria-expanded, data-active) rather than text content.
func BindAttr(html render.HTML, signal, attr string) render.HTML {
	return bindSignal(html, signal, "attr", attr)
}

// bindSignal injects the signal attribute pair (plus the attr name for
// mode="attr") in sorted order: data-fui-signal, then data-fui-signal-attr,
// then data-fui-signal-mode. Appending in that order keeps the output
// byte-identical to a sorted render.Tag attrs map.
func bindSignal(html render.HTML, signal, mode, attr string) render.HTML {
	out := injectAttr(html, "data-fui-signal", signal)
	if attr != "" {
		out = injectAttr(out, "data-fui-signal-attr", attr)
	}
	out = injectAttr(out, "data-fui-signal-mode", mode)
	return out
}
