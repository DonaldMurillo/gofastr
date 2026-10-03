package headless

import (
	"strings"
)

// Island is where a state change goes: the endpoint that renders the
// region again, and the signal that region is bound to.
//
// It is the sibling of Button's Action, and it exists for the same
// reason: the host framework attaches a request to markup by splicing
// attributes into the rendered string, which works on anything and
// therefore says nothing. Here the request is a prop on the component
// that fires it, so a table that turns its own pages says so in its
// props, in one place, and the attributes cannot be forged through
// ExtraAttrs — Safe drops every data-fui-* key on purpose.
//
// The framework's first hard rule is that an in-page state change is
// never a route: no link that merely navigates for a new page of rows,
// a new sort, a filter. The component renders the RPC contract (see
// attrs, in box.go) on the SAME element that keeps the href or action
// for no script, so the progressive shape is one element — an anchor
// that is the page without script and the island update with it.
type Island struct {
	// Endpoint is the RPC path the region's next rendering comes
	// from: same-origin, starting with "/".
	Endpoint string
	// Signal is the data-fui-signal the region is bound to; the
	// response replaces it.
	Signal string
}

// check refuses an island that looks wired and is not: no endpoint, a
// cross-origin one the runtime will silently decline to fetch (the
// same rule as checkActionEndpoint), or no signal for the response to
// land in — which is a region that never updates.
func (i Island) check() {
	if i.Endpoint == "" {
		panic("headless: an Island needs an Endpoint — the path that renders the region again")
	}
	checkSameOrigin("an Island", "Endpoint", i.Endpoint)
	if i.Signal == "" {
		panic("headless: an Island needs a Signal — the data-fui-signal the region is bound to")
	}
	checkSignalName(i.Signal)
}

// checkSameOrigin refuses an endpoint the runtime would decline to
// fetch, at render, where the mistake is a panic with a reason rather
// than a dead control in production. Same-origin means a path: it
// starts with one slash and not two, because "//host/…" is
// protocol-relative and therefore cross-origin, and not with a
// backslash either, because the URL parser reads "/\host/…" as the
// same thing.
func checkSameOrigin(component, what, endpoint string) {
	if !strings.HasPrefix(endpoint, "/") || strings.HasPrefix(endpoint, "//") || strings.HasPrefix(endpoint, "/\\") {
		panic("headless: " + component + " " + what + " must be same-origin and start with /, not " + endpoint)
	}
}

// zero reports whether no island was given at all, so an optional
// Island (Form's) can leave the plain render alone.
func (i Island) zero() bool { return i.Endpoint == "" && i.Signal == "" }

// requireIsland is the framework's hard rule 1 enforced at render: a
// component whose whole purpose is an in-page state change — turning a
// page, sorting a column, applying a filter — refuses to render as the
// link-only navigation the rule forbids. The island target is
// required, so that render cannot be built.
func requireIsland(component string, i Island) {
	if i.zero() {
		panic("headless: " + component + " requires Island — an in-page state change is never a route " +
			"(the framework's hard rule 1); give the Island that renders this region")
	}
	i.check()
}
