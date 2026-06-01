// Package interactive provides declarative interactivity primitives for
// GoFastr components. It wraps arbitrary render.HTML with data-fui-*
// attributes the runtime understands — RPC calls, signal bindings, widget
// chaining — without writing any JavaScript.
//
// Usage:
//
//	interactive.OnClick(btn, interactive.Post("/api/like"),
//	    interactive.OnSuccess(interactive.SetSignal("count", "response")),
//	)
//
// The package only emits attributes the runtime already handles
// (data-fui-rpc, data-fui-signal, data-fui-open, etc.) plus new ones
// added for chaining (data-fui-rpc-open, data-fui-rpc-signals).
package interactive

import (
	"fmt"
	"strings"

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
	wrapped = injectAttr(wrapped, "data-fui-rpc-debounce", fmt.Sprintf("%d", ms))
	return wrapped
}


// ─── Scroll-triggered reveal ────────────────────────────────────────

// Reveal wraps an element so it animates in when it enters the viewport.
// The animationType determines the CSS class added on reveal
// ("fade-up", "fade-in", "slide-left", etc.).
// If animationType is empty, "fade-in" is used as the default.
func Reveal(html render.HTML, animationType string) render.HTML {
	if animationType == "" {
		animationType = "fade-in"
	}
	return injectAttr(html, "data-fui-reveal", animationType)
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
	return render.Tag("div", map[string]string{
		"data-fui-dropdown-wrap": "",
	}, wrappedTrigger, wrappedPanel)
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
		"data-fui-comp":               "ui-optimistic-action",
		"data-state":                  "idle",
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
	for _, e := range a.effects {
		for k, v := range e.rpcAttrs() {
			m[k] = v
		}
	}
	return m
}
