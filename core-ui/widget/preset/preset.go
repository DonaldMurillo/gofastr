// Package preset bundles the most common widget surfaces as opinionated
// builders on top of widget.Definition. Each is a one-call shortcut so
// hosts don't reach for the raw builder for normal cases.
//
// Every preset returns a *widget.Builder that the host can further
// customize (slots, signals, RPCs) before .Build()ing.
package preset

import (
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FloatingPanel is a corner-anchored, persistent panel — useful for
// chat surfaces, devtools, build status. Defaults to BottomRight.
// Backdrop=false (it floats above the page without dimming it).
func FloatingPanel(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.BottomRight)
}

// Modal is a center-mounted dialog with a backdrop. Closes on ESC and
// click-outside by default. Use for confirmation dialogs, agent-config
// pickers, "are you sure?" flows.
func Modal(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Center)
}

// Toast is an ephemeral notification anchored to the bottom. The host
// SHOULD set up an SSE binding so server-side events push messages
// without page reloads.
//
// Deprecated: prefer ToastStack for new code — it bundles a stack
// manager, signal binding, and SSE wiring in one call. Toast is kept
// for the bottom-anchored single-pill pattern callers may already
// depend on.
func Toast(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Bottom)
}

// ToastStack registers an empty toast stack widget. Toasts are
// pushed entirely on the client — either via the JS API
// `window.__gofastr.toast({...})`, or by setting an
// `X-Gofastr-Toast: <json>` header on the response of any
// `data-fui-rpc` handler. The runtime appends the rendered item into
// this widget's stack container and handles the TTL / dismiss
// lifecycle.
//
// No SSE, no server-side queue, no per-page connection cost. The
// stack lives client-side; reload starts fresh.
//
// Wiring is a one-liner:
//
//	stack := preset.ToastStack("site-toasts").Build()
//	widget.Mount(r, &stack)
//
// Server code that wants to surface a toast on an RPC response:
//
//	ui.AddToastSuccess(w, "Saved", "Your changes are persisted.", 5000)
//
// Or from the client:
//
//	window.__gofastr.toast({variant:"success", title:"Saved", ttl:5000});
//
// Position can be overridden after the preset returns. Backdrop is
// intentionally OFF — toasts are non-blocking.
func ToastStack(name string) *widget.Builder {
	// No custom Skeleton — the framework's defaultSkeleton picks up
	// whatever Position the caller chose via .Mount(). The slot
	// renders the empty `data-fui-toast-stack="<name>"` container
	// the runtime appends items into.
	return widget.New(name).
		Mount(widget.TopRight).
		Slot("items", clientToastSlot{name: name})
}

// clientToastSlot renders the empty stack container. The runtime
// appends items into this element when a toast fires.
type clientToastSlot struct{ name string }

func (s clientToastSlot) Render() render.HTML {
	return render.HTML(
		`<div class="ui-toast-stack" data-fui-comp="ui-toast-stack" data-fui-toast-stack="` +
			s.name + `"></div>`,
	)
}

var _ component.Component = clientToastSlot{}

// Drawer is an edge-mounted sliding panel. Defaults to the left edge;
// pass widget.EdgeRight to flip. Includes a backdrop, closes on
// Escape, and closes on click-outside — matches Modal's dismiss
// affordances. Set the corresponding fields back to false after
// .Build() if a drawer needs to behave as a non-dismissible panel.
//
// Role defaults to "dialog" so screen readers announce the drawer
// like a modal — overrideable via .Role("…") on the builder.
func Drawer(name string) *widget.Builder {
	b := widget.New(name).
		Mount(widget.Edge).
		Backdrop().
		Role("dialog")
	// Match Modal's CloseOnEscape / CloseOnClickOutside since drawers
	// are equally modal in feel. widget.Builder doesn't expose setters
	// for these (only the Center-position Mount auto-enables them), so
	// reach into the definition directly.
	d := b.Definition()
	d.CloseOnEscape = true
	d.CloseOnClickOutside = true
	return b
}

// Banner is a top-anchored persistent strip. Useful for build progress,
// "agent thinking" indicators, version-mismatch warnings.
func Banner(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Top)
}
