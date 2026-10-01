package app

// The typed outlet handle (docs/DESIGN-layout-outlets.md, "API"): an
// outlet is a value the layout owns, and a screen or group fills one
// by value — the way an embed surface holds its *Screen rather than a
// path string. A mistyped outlet (shell.Tolbar) does not compile; a
// fill for a layout outside the screen's chain panics at render
// naming the layout and the outlet; binding one handle to two
// layouts panics at NewLayout.

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Outlet is a typed handle to one declared outlet of one tree layout.
// The author holds handles — typically as struct fields beside the
// *Layout — and passes them to NewLayout (which claims ownership),
// Screen.Fill / ScreenGroup.Fill and LayoutTree.Place. The embedded
// OutletSpec carries the declaration; NewOutlet is the constructor.
type Outlet struct {
	OutletSpec

	// owner is the layout this handle was declared by (set by
	// NewLayout, which refuses a handle already bound). Nil until
	// then: a fill or placement of an ownerless handle is refused by
	// the render-time fill validation.
	owner *Layout
}

// Name returns the outlet's declared name.
func (o *Outlet) Name() string { return o.OutletSpec.Name }

// OutletOptions configures one outlet handle (NewOutlet). The zero
// value declares an empty FallbackNothing outlet.
type OutletOptions struct {
	// Default is a fill candidate of last resort: when neither the
	// screen nor any group fills the outlet, Default renders.
	Default component.Component
	// Fallback decides the unfilled rendering when Default is nil.
	// FallbackNothing (the zero value) renders nothing;
	// FallbackNotFound makes the whole render answer 404 (Decided 5).
	// Refused beside a Default (NewLayout panics).
	Fallback OutletFallback
	// Transition is this outlet cell's view transition.
	Transition Transition
	// Transitions is the per-request set; TransitionFor picks this
	// render's entry. Setting either beside the static Transition is
	// a mount error (NewLayout panics).
	Transitions   map[string]Transition
	TransitionFor func(ctx context.Context) string
	// Deferred moves this outlet's fill off the page request: it
	// arrives as its own part request (X-Gofastr-Part).
	Deferred bool
	// Policy is the outlet's region guard.
	Policy Policy
	// Loading declares what the outlet shows while a navigation that
	// will change it is in flight.
	Loading *Loading
}

// validOutletName reports whether name is a legal outlet name:
// letters, digits, '-' and '_' (DESIGN "API"). The name lands in a
// data-fui-* attribute value and a "#" address, so spaces, '#' and
// '~' are refused outright rather than escaped.
func validOutletName(name string) bool {
	if name == "" {
		return false
	}
	for _, c := range name {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}

// NewOutlet creates an outlet handle. The name must be letters,
// digits, '-' and '_' (NewOutlet panics on anything else — the name
// is a wire address, not prose). opts configure the outlet; nothing
// else is required.
func NewOutlet(name string, opts ...OutletOptions) *Outlet {
	if !validOutletName(name) {
		panic(fmt.Sprintf("app: outlet name %q is invalid (allowed: letters, digits, '-', '_')", name))
	}
	o := &Outlet{OutletSpec: OutletSpec{Name: name}}
	for _, opt := range opts {
		o.Default = firstNonNil(opt.Default, o.Default)
		if opt.Fallback != FallbackNothing {
			o.Fallback = opt.Fallback
		}
		if !opt.Transition.isZero() {
			o.Transition = opt.Transition
		}
		if len(opt.Transitions) > 0 {
			o.Transitions = opt.Transitions
		}
		if opt.TransitionFor != nil {
			o.TransitionFor = opt.TransitionFor
		}
		if opt.Deferred {
			o.Deferred = true
		}
		if opt.Policy != nil {
			o.Policy = opt.Policy
		}
		if opt.Loading != nil {
			o.Loading = opt.Loading
		}
	}
	return o
}

func firstNonNil(a, b component.Component) component.Component {
	if a != nil {
		return a
	}
	return b
}
