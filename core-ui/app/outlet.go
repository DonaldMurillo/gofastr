package app

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// Outlet is a typed handle to one declared outlet of one tree layout.
type Outlet struct {
	OutletSpec

	owner *Layout
}

// Name returns the outlet's declared name.
func (o *Outlet) Name() string { return o.OutletSpec.Name }

// OutletOptions configures one outlet handle (NewOutlet).
type OutletOptions struct {
	// Default is a fill candidate of last resort: when neither the screen nor any group fills the
	// outlet, Default renders.
	Default component.Component
	// Fallback decides the unfilled rendering when Default is nil.
	Fallback OutletFallback
	// Transition is this outlet cell's view transition.
	Transition Transition
	// Transitions is the per-request set; TransitionFor picks this render's entry.
	Transitions   map[string]Transition
	TransitionFor func(ctx context.Context) string
	// Deferred moves this outlet's fill off the page request: it arrives as its own part request
	// (X-Gofastr-Part).
	Deferred bool
	// Policy is the outlet's region guard.
	Policy Policy
	// Loading declares what the outlet shows while a navigation that will change it is in flight.
	Loading *Loading
}

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

// NewOutlet creates an outlet handle.
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
