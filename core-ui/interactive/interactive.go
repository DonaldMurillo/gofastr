// Package interactive wires RPC calls, signal bindings and widget triggers onto
// server-rendered HTML as data-cui-* attributes the runtime reads.
package interactive

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/internal/uiaction"
)

// Action describes an RPC call triggered by click or submit.
type Action struct {
	method        string  // GET, POST, PUT, DELETE, PATCH
	path          string  // URL path
	confirm       Confirm // pre-flight confirmation (empty Message = none)
	body          string  // static JSON body for non-form RPCs (data-cui-rpc-body); empty = none
	effects       []Effect
	errorToast    *string
	successToast  *string
	successAction *ToastAction
}

// ToastAction is a toast's one button as the runtime reads it: its label and the RPC attributes it
// carries.
type ToastAction uiaction.ToastAction

// NewToastAction is a toast button labelled label that runs next.
func NewToastAction(label string, next Action) *ToastAction {
	action := uiaction.ToastAction{Label: label, Attrs: next.attrs()}
	if err := action.Validate(); err != nil {
		panic("interactive: " + err.Error())
	}
	return (*ToastAction)(&action)
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
	if len(effects) > 0 {
		a.effects = slices.Concat(a.effects, effects)
	}
	return a
}

// Confirm is what a pre-flight confirmation says: the dialog's title, its message, the accept
// button's label, and whether accepting destroys something (Danger draws the accept button in the
// danger variant).
type Confirm struct {
	Title   string
	Message string
	Accept  string
	Danger  bool
}

// WithConfirm gates the action behind a PRE-FLIGHT confirmation.
func (a Action) WithConfirm(message string) Action {
	return a.WithConfirmDialog(Confirm{Message: message})
}

// WithConfirmDialog gates the action behind the kit's confirm dialog, spelled in full.
func (a Action) WithConfirmDialog(c Confirm) Action {
	if c.Message == "" {
		panic("interactive: WithConfirmDialog needs a Message — a confirmation that says nothing asks nothing")
	}
	a.confirm = c
	return a
}

// OnErrorToast makes a refused or failed RPC visible: on a non-2xx answer (or a network error) the
// runtime shows an error toast titled title, with the server's JSON "error" message as its body.
func (a Action) OnErrorToast(title string) Action {
	a.errorToast = &title
	return a
}

// OnSuccessToast shows a success toast titled title on a 2xx answer, before any OnSuccess(Navigate)
// runs.
func (a Action) OnSuccessToast(title string) Action {
	a.successToast = &title
	return a
}

// OnSuccessToastAction puts one action on the OnSuccessToast toast: a button labelled label that
// runs next when pressed, with next's own toasts and navigation.
func (a Action) OnSuccessToastAction(label string, next Action) Action {
	a.successAction = NewToastAction(label, next)
	return a
}

// WithBody attaches a static JSON body to the action, the payload sent for a non-form RPC (a button
// click that isn't inside a <form>).
func (a Action) WithBody(body string) Action {
	if !json.Valid([]byte(body)) {
		panic(fmt.Sprintf("interactive: WithBody requires valid JSON, got %q", body))
	}
	a.body = body
	return a
}

// Effect is something that happens after an RPC response.
type Effect interface {
	rpcAttrs() map[string]string
}

// SetSignal pushes the RPC response into a named client-side signal.
func SetSignal(name string) Effect {
	if strings.ContainsRune(name, '"') {
		panic(fmt.Sprintf("interactive: signal name must not contain '\"', got %q", name))
	}
	refuseReservedSignalName(name)
	return signalEffect{name: name}
}

func refuseReservedSignalName(name string) {
	if name == "__proto__" || name == "constructor" || name == "prototype" {
		panic(fmt.Sprintf("interactive: signal name %q is reserved — the runtime kernel refuses every write to __proto__/constructor/prototype, so the control would never fire", name))
	}
}

type signalEffect struct{ name string }

func (e signalEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-signal": e.name}
}

// OpenWidget opens a named widget when the RPC succeeds.
func OpenWidget(name string) Effect {
	return openWidgetEffect{name: name}
}

type openWidgetEffect struct{ name string }

func (e openWidgetEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-open": e.name}
}

// CloseWidget closes the enclosing widget on RPC success.
func CloseWidget() Effect {
	return closeWidgetEffect{}
}

type closeWidgetEffect struct{}

func (e closeWidgetEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-close": "true"}
}

// ResetForm resets the enclosing form on RPC success.
func ResetForm() Effect {
	return resetFormEffect{}
}

type resetFormEffect struct{}

func (e resetFormEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-reset": "true"}
}

// Navigate does an SPA navigation on RPC success.
func Navigate(path string) Effect {
	return navigateEffect{path: path}
}

type navigateEffect struct{ path string }

func (e navigateEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-navigate": e.path}
}

// AfterText replaces the trigger element's text content with text on 2xx RPC success.
func AfterText(text string) Effect {
	return afterTextEffect{text: text}
}

type afterTextEffect struct{ text string }

func (e afterTextEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-after-text": e.text}
}

// AfterDisable permanently disables the trigger element on 2xx RPC success (sets aria-
// disabled="true" and, for buttons/inputs, disabled=true).
func AfterDisable() Effect {
	return afterDisableEffect{}
}

type afterDisableEffect struct{}

func (e afterDisableEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-after-disable": ""}
}

// ScrollTo smooth-scrolls the element matching selector into view on 2xx RPC success.
func ScrollTo(selector string) Effect {
	return scrollToEffect{selector: selector}
}

type scrollToEffect struct{ selector string }

func (e scrollToEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-rpc-scroll-to": e.selector}
}

// PushState applies a URL update via history.pushState after 2xx RPC success without triggering a
// fetch.
func PushState(path string) Effect {
	return pushStateEffect{path: path}
}

type pushStateEffect struct{ path string }

func (e pushStateEffect) rpcAttrs() map[string]string {
	return map[string]string{"data-cui-push-state": e.path}
}

// OnClick wraps an HTML element so that clicking it fires the action.
func OnClick(html render.HTML, action Action) render.HTML {
	return wrapWithAction(html, action)
}

// OnSubmit wraps a form element so that submitting it fires the action.
func OnSubmit(html render.HTML, action Action) render.HTML {
	return wrapWithAction(html, action)
}

// LiveSearch wraps a form element so input changes fire debounced RPCs.
func LiveSearch(form render.HTML, action Action, debounceMs int) render.HTML {
	wrapped := wrapWithAction(form, action)
	wrapped = injectAttr(wrapped, "data-cui-rpc-trigger", "input")
	ms := debounceMs
	if ms == 0 {
		ms = 250
	}
	wrapped = injectAttr(wrapped, "data-cui-rpc-debounce-ms", fmt.Sprintf("%d", ms))
	return wrapped
}

var revealStyle = registry.RegisterStyle("cui-reveal", revealCSS)

// Reveal wraps an element so it animates in when it enters the viewport.
func Reveal(html render.HTML, animationType string) render.HTML {
	if animationType == "" {
		animationType = "fade-in"
	}
	const name = "cui-reveal"
	out := injectAttr(html, "data-cui-reveal", animationType)
	if comp, hasComp, _ := registry.Attribute(out, "data-cui-comp"); !hasComp || comp == name {
		return revealStyle.WrapHTML(out)
	}
	scope, hasScope, _ := registry.Attribute(out, "data-cui-scope")
	if !hasScope || scope == name {
		scoped, _ := registry.InjectAttribute(out, "data-cui-scope", name)
		return scoped
	}
	return revealStyle.WrapHTML(render.Tag("div", map[string]string{"data-cui-reveal": animationType}, html))
}

func revealCSS(_ style.Theme) string {
	const root = `:is([data-cui-comp="cui-reveal"],[data-cui-scope="cui-reveal"])`
	const target = root + `[data-cui-reveal]`
	return target + `{opacity:1}` +
		target + `.cui-hidden{opacity:0}` +
		root + `[data-cui-reveal="fade-up"].cui-hidden{transform:translateY(24px)}` +
		root + `[data-cui-reveal="slide-left"].cui-hidden{transform:translateX(24px)}` +
		root + `[data-cui-reveal="slide-right"].cui-hidden{transform:translateX(-24px)}` +
		target + `.cui-revealed{opacity:1;transform:none;transition:opacity .6s var(--easing-ease-in-out, ease),transform .6s var(--easing-ease-in-out, ease)}` +
		`@media (prefers-reduced-motion:reduce){` + target + `.cui-hidden{opacity:1;transform:none}` + target + `.cui-revealed{transition:none}}`
}

// SetLocal wraps an HTML element so clicking it sets a signal to a fixed value.
func SetLocal(html render.HTML, signalName, value string) render.HTML {
	refuseReservedSignalName(signalName)
	return injectAttr(html, "data-cui-signal-set", signalName+":"+value)
}

// IncLocal wraps an HTML element so clicking it increments a numeric signal by delta (default 1).
func IncLocal(html render.HTML, signalName string, delta int) render.HTML {
	refuseReservedSignalName(signalName)
	val := signalName
	if delta != 1 {
		val = fmt.Sprintf("%s:%d", signalName, delta)
	}
	return injectAttr(html, "data-cui-signal-inc", val)
}

// ToggleLocal wraps an HTML element so clicking it toggles a boolean signal.
func ToggleLocal(html render.HTML, signalName string) render.HTML {
	refuseReservedSignalName(signalName)
	return injectAttr(html, "data-cui-signal-toggle", signalName)
}

// Dropdown wraps a trigger and a panel into a click-toggle dropdown.
var dropdownStyle = registry.RegisterStyle("cui-dropdown", dropdownCSS)

func Dropdown(trigger, panel render.HTML) render.HTML {
	triggerAttrs := map[string]string{
		"data-cui-dropdown": "",
		"aria-expanded":     "false",
		"aria-haspopup":     "true",
	}
	panelAttrs := map[string]string{
		"data-cui-dropdown-panel": "",
		"hidden":                  "",
	}
	wrappedTrigger := registry.AppendAttributes(trigger, triggerAttrs)
	wrappedPanel := registry.AppendAttributes(panel, panelAttrs)
	wrap := render.Tag("div", map[string]string{
		"data-cui-dropdown-wrap": "",
	}, wrappedTrigger, wrappedPanel)
	return dropdownStyle.WrapHTML(wrap)
}

func dropdownCSS(_ style.Theme) string {
	return `[data-cui-comp="cui-dropdown"]{position:relative;display:inline-block}` +
		`[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel]{position:absolute;top:calc(100% + var(--spacing-sm, 4px));inset-inline-start:0;min-width:var(--ui-dropdown-min-width, 11rem);background:var(--fui-surface, var(--color-surface, #fff));border:var(--stroke-thin, 1px) solid var(--fui-border, var(--color-border, #E4E4E7));border-radius:var(--radii-md, 8px);box-shadow:var(--shadow-md, 0 8px 24px rgba(0,0,0,.12));padding:var(--spacing-sm, .25rem);z-index:var(--z-dropdown, 100)}` +
		`[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel] a,[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel] button{display:block;width:100%;box-sizing:border-box;text-align:start;padding:var(--spacing-md, .5rem) calc(var(--spacing-sm, 4px) * 3);border-radius:var(--radii-sm, 6px);color:var(--fui-foreground, var(--color-text, #09090B));text-decoration:none;background:none;border:none;cursor:pointer;font:inherit;font-size:var(--text-sm, .875rem)}` +
		`[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel] a:hover,[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel] button:hover{background:var(--fui-muted-bg, var(--color-surface-soft, #F4F4F5))}` +
		`[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel] a:focus-visible,[data-cui-comp="cui-dropdown"] [data-cui-dropdown-panel] button:focus-visible{outline:var(--stroke-focus, 2px) solid var(--color-text-subtle);outline-offset:calc(-1 * var(--stroke-focus-offset, 2px))}`
}

// AnimateOnSignal wraps an element so it gets a CSS class when a signal is truthy and loses it when
// falsy.
func AnimateOnSignal(html render.HTML, signalName, cssClass string) render.HTML {
	if signalName == "" {
		panic("interactive: AnimateOnSignal signalName must not be empty")
	}
	if cssClass == "" {
		panic("interactive: AnimateOnSignal cssClass must not be empty")
	}
	refuseReservedSignalName(signalName)
	html = injectAttr(html, "data-cui-animate-signal", signalName)
	html = injectAttr(html, "data-cui-animate-class", cssClass)
	return html
}

// EditToggle wraps an element so clicking it toggles a boolean signal.
func EditToggle(html render.HTML, signalName string) render.HTML {
	return ToggleLocal(html, signalName)
}

// CancelEdit wraps an element so clicking it sets a signal to false, closing the inline-edit mode.
func CancelEdit(html render.HTML, signalName string) render.HTML {
	return SetLocal(html, signalName, "false")
}

func OptimisticUpdate(action Action, idle, success render.HTML) render.HTML {
	for _, key := range slices.Sorted(maps.Keys(action.attrs())) {
		if key != "data-cui-rpc" && key != "data-cui-rpc-method" {
			panic(fmt.Sprintf("interactive: OptimisticUpdate cannot carry %s; use OnClick for configured RPC actions", key))
		}
	}
	button := uiaction.Button{
		Endpoint: action.path, Method: action.method, State: "idle", FailedText: uiaction.DefaultFailedText,
		Idle: idle, Done: success,
	}
	return button.Render(map[string]string{}, func(tag, _ string, attrs map[string]string, children ...render.HTML) render.HTML {
		return render.Tag(tag, attrs, children...)
	})
}

func injectAttr(html render.HTML, key, value string) render.HTML {
	return registry.AppendAttributes(html, map[string]string{key: value})
}

func wrapWithAction(html render.HTML, action Action) render.HTML {
	return registry.AppendAttributes(html, action.attrs())
}

func (a Action) attrs() map[string]string {
	if a.method == "" && a.path == "" {
		return nil
	}
	m := map[string]string{
		"data-cui-rpc":        a.path,
		"data-cui-rpc-method": a.method,
	}
	if a.confirm.Message != "" {
		m["data-cui-confirm"] = a.confirm.Message
		if a.confirm.Title != "" {
			m["data-cui-confirm-title"] = a.confirm.Title
		}
		if a.confirm.Accept != "" {
			m["data-cui-confirm-accept"] = a.confirm.Accept
		}
		if a.confirm.Danger {
			m["data-cui-confirm-tone"] = "danger"
		}
	}
	if a.body != "" {
		m["data-cui-rpc-body"] = a.body
	}
	if a.errorToast != nil {
		m["data-cui-rpc-error-toast"] = *a.errorToast
	}
	if a.successToast != nil {
		m["data-cui-rpc-success-toast"] = *a.successToast
	}
	if a.successAction != nil {
		b, _ := json.Marshal(a.successAction)
		m["data-cui-rpc-success-action"] = string(b)
	}
	if uiaction.MissingSuccessToast(m) {
		panic("interactive: OnSuccessToastAction needs OnSuccessToast — the action rides the success toast")
	}
	for _, e := range a.effects {
		maps.Copy(m, e.rpcAttrs())
	}
	return m
}

// Attrs returns the data-cui-* attributes this Action would inject, as a plain map[string]string.
func (a Action) Attrs() map[string]string {
	return a.attrs()
}

// OpenOnClick wraps an HTML element so clicking it opens a registered widget surface.
func OpenOnClick(html render.HTML, widget string) render.HTML {
	return injectAttr(html, "data-cui-open", widget)
}

// Toast is the config for a click-fired toast notification.
type Toast struct {
	Variant string `json:"variant,omitempty"` // "success" | "warning" | "danger" | "info" | "neutral"; defaults to "info"
	Title   string `json:"title,omitempty"`   // required by the runtime, a toast with no title is dropped
	Body    string `json:"body,omitempty"`    // optional supporting copy
	Stack   string `json:"stack,omitempty"`   // named [data-cui-toast-stack] container; empty → the auto stack
	TTLMs   int    `json:"ttl,omitempty"`     // auto-dismiss delay in ms; 0 → persistent (manual dismiss only)
}

// ToastOnClick wraps an HTML element so clicking it fires a toast with the given config.
func ToastOnClick(html render.HTML, t Toast) render.HTML {
	return injectAttr(html, "data-cui-toast", marshalToast(t))
}

func marshalToast(t Toast) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(t); err != nil {
		panic(fmt.Sprintf("interactive: failed to marshal toast: %v", err))
	}
	return strings.TrimSpace(buf.String())
}

var validPanes = map[string]bool{"secondary": true, "tertiary": true}

// OpenPaneOnClick wraps an HTML element so clicking it opens the named side pane.
func OpenPaneOnClick(html render.HTML, pane string) render.HTML {
	if !validPanes[pane] {
		panic(fmt.Sprintf("interactive: OpenPaneOnClick pane must be \"secondary\" or \"tertiary\", got %q", pane))
	}
	return injectAttr(html, "data-hui-pane-open-control", pane)
}

// ClosePaneOnClick wraps an HTML element so clicking it closes a side pane.
func ClosePaneOnClick(html render.HTML, pane string) render.HTML {
	if pane != "" && !validPanes[pane] {
		panic(fmt.Sprintf("interactive: ClosePaneOnClick pane must be \"secondary\", \"tertiary\", or \"\" (topmost), got %q", pane))
	}
	return injectAttr(html, "data-hui-pane-close", pane)
}

// SwapPaneOnClick wraps an HTML element so clicking it opens the named side pane and closes its
// sibling (one open pane at a time).
func SwapPaneOnClick(html render.HTML, pane string) render.HTML {
	if !validPanes[pane] {
		panic(fmt.Sprintf("interactive: SwapPaneOnClick pane must be \"secondary\" or \"tertiary\", got %q", pane))
	}
	return injectAttr(html, "data-hui-pane-swap", pane)
}

// PaneKey labels a pane trigger with the identity of what it opens, the ticket id, the record slug,
// whatever the pane will show.
func PaneKey(html render.HTML, key string) render.HTML {
	return injectAttr(html, "data-hui-pane-key", key)
}

// BindHTML wraps an HTML region whose innerHTML is replaced with the signal value (the trusted-HTML
// path).
func BindHTML(html render.HTML, signal string) render.HTML {
	return bindSignal(html, signal, "html", "")
}

// BindText wraps an HTML element whose textContent tracks a signal (HTML-escaped).
func BindText(html render.HTML, signal string) render.HTML {
	return bindSignal(html, signal, "text", "")
}

// BindAttr wraps an HTML element whose attribute tracks a signal.
func BindAttr(html render.HTML, signal, attr string) render.HTML {
	if !SignalAttrAllowed(attr) {
		return html
	}
	return bindSignal(html, signal, "attr", attr)
}

func bindSignal(html render.HTML, signal, mode, attr string) render.HTML {
	refuseReservedSignalName(signal)
	out := injectAttr(html, "data-cui-signal", signal)
	if attr != "" {
		out = injectAttr(out, "data-cui-signal-attr", attr)
	}
	out = injectAttr(out, "data-cui-signal-mode", mode)
	return out
}
