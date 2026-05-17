package ui

import (
	"encoding/json"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Toast surface: client-driven, no SSE.
//
// Two trigger paths reach the same client-side stack:
//
//  1. **Frontend**: `window.__gofastr.toast({variant, title, body, ttl})`
//     called from inline JS or component bindings.
//  2. **Server response header**: any HTTP response — typically an RPC
//     handler's reply — can set `X-Gofastr-Toast: <json>`; the runtime
//     scans every `data-fui-rpc` response for the header and dispatches
//     it through the same client API.
//
// Both paths converge on `__gofastr.toast(cfg)`, which builds the item
// HTML inline from a small template, appends it to the stack
// container, and wires the TTL + dismiss handlers. No server-side
// queue, no SSE connection — the stack lives entirely in the browser.
//
// Use AddToast from server handlers; use TriggerHeader / TriggerJSON
// when composing the header value manually.

// ToastTrigger is the JSON shape carried by the X-Gofastr-Toast
// header — and the same shape accepted by __gofastr.toast(cfg) on
// the client. Field names match the runtime template; keep both in
// sync when extending.
type ToastTrigger struct {
	Variant StatusVariant `json:"variant,omitempty"` // info | success | warning | danger | neutral
	Title   string        `json:"title"`             // required
	Body    string        `json:"body,omitempty"`
	TTL     int           `json:"ttl,omitempty"` // milliseconds; 0 = persistent
	// Stack is the name of the toast stack widget to push into.
	// Defaults to the first stack mounted on the page. Set explicitly
	// when an app hosts multiple stacks (e.g. per-tenant).
	Stack string `json:"stack,omitempty"`
}

// AddToast appends a toast trigger to the X-Gofastr-Toast response
// header. The runtime fires the toast on the client when the matching
// data-fui-rpc fetch resolves with 2xx.
//
// Multiple AddToast calls accumulate into a single header whose value
// is a JSON array — robust against fetch's header-value coalescing
// across browsers. Apps that need to surface several toasts from one
// handler just call AddToast multiple times.
//
// Apps can call this from any HTTP handler that's reached via
// data-fui-rpc; the toast travels back on the response that the
// runtime is already waiting for, with no extra request.
func AddToast(w http.ResponseWriter, t ToastTrigger) {
	if t.Title == "" {
		return
	}
	if t.Variant == "" {
		t.Variant = StatusInfo
	}
	existing := w.Header().Get("X-Gofastr-Toast")
	var list []ToastTrigger
	if existing != "" {
		// Tolerate either an array (the canonical form we emit) or a
		// single object (a previous caller may have set it manually).
		if existing[0] == '[' {
			_ = json.Unmarshal([]byte(existing), &list)
		} else {
			var single ToastTrigger
			if json.Unmarshal([]byte(existing), &single) == nil {
				list = append(list, single)
			}
		}
	}
	list = append(list, t)
	enc, err := json.Marshal(list)
	if err != nil {
		return
	}
	w.Header().Set("X-Gofastr-Toast", string(enc))
}

// AddToastSuccess / AddToastError / AddToastWarning are sugar for the
// common cases. ttlMs of 0 means persistent (caller must dismiss).
func AddToastSuccess(w http.ResponseWriter, title, body string, ttlMs int) {
	AddToast(w, ToastTrigger{Variant: StatusSuccess, Title: title, Body: body, TTL: ttlMs})
}

func AddToastError(w http.ResponseWriter, title, body string) {
	AddToast(w, ToastTrigger{Variant: StatusDanger, Title: title, Body: body, TTL: 0})
}

func AddToastWarning(w http.ResponseWriter, title, body string, ttlMs int) {
	AddToast(w, ToastTrigger{Variant: StatusWarning, Title: title, Body: body, TTL: ttlMs})
}

// toastSlot is the widget Slot component that renders the initial
// empty stack container. The client-side `__gofastr.toast(cfg)` and
// header-driven flow both append into this container.
type toastSlot struct{ name string }

func (t toastSlot) Render() render.HTML {
	return render.HTML(
		`<div class="ui-toast-stack" data-fui-comp="ui-toast-stack" data-fui-toast-stack="` +
			escAttr(t.name) + `"></div>`,
	)
}

var _ component.Component = toastSlot{}

// ToastSlot exposes a fresh empty-stack slot Component for callers
// composing a preset.ToastStack(name, ToastSlot(name)) manually.
// preset.ToastStack uses it internally; this is exported so a host can
// build its own custom layout while sharing the runtime contract.
func ToastSlot(name string) component.Component { return toastSlot{name: name} }

// SignalSource for the toast stack — returns the empty container.
// Surface state is purely client-side; this exists so the widget
// /state endpoint stays a no-op rather than 404.
type toastStackSource struct{ name string }

func (t toastStackSource) Read() (any, error) {
	return string(toastSlot{name: t.name}.Render()), nil
}

// ToastStackSignal returns a SignalSource that emits the empty stack
// container. Wired into preset.ToastStack automatically; exported for
// hosts composing their own widget.
func ToastStackSignal(name string) widget.SignalSource { return toastStackSource{name: name} }
