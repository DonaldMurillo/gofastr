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

// wrapWithAction merges action attributes into the outermost HTML tag.
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
