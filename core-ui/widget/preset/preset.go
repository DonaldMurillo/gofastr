// Package preset bundles the most common widget surfaces as opinionated
// builders on top of widget.Definition. Each is a one-call shortcut so
// hosts don't reach for the raw builder for normal cases.
//
// Every preset returns a *widget.Builder that the host can further
// customize (slots, signals, RPCs) before .Build()ing.
package preset

import "github.com/gofastr/gofastr/core-ui/widget"

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
func Toast(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Bottom)
}

// Drawer is an edge-mounted sliding panel. Defaults to the left edge;
// pass widget.EdgeRight to flip. Includes a backdrop.
func Drawer(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Edge).
		Backdrop()
}

// Banner is a top-anchored persistent strip. Useful for build progress,
// "agent thinking" indicators, version-mismatch warnings.
func Banner(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Top)
}
