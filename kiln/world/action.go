package world

import (
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/node"
)

// supportedActionKinds is the closed set of action kinds that kiln/effect
// actually evaluates. An action whose Kind is not in this set is
// advertised-but-unimplemented; the journal rejects it at authoring time
// (see [ValidateAction]) so it can never land in the world IR and silently
// no-op, or 500, when a hook, route, or page element tries to fire it.
//
// Keep this set in sync with the dispatch switches in kiln/effect (Run and
// Resolve). The set lives here, in the leaf world package, because every
// action-bearing structure (Hook, Route, EntityEndpoint, page tree) is a
// world type; effect re-checks at execution time as the last line of defence.
var supportedActionKinds = map[string]bool{
	ActionNoop:        true,
	ActionValidate:    true,
	ActionSetField:    true,
	ActionAudit:       true,
	ActionRespondJSON: true,
	ActionEmitEvent:   true,
}

// ValidateAction is the authoring-time scream that keeps unsupported action
// kinds out of the world IR. It rejects anything kiln/effect cannot run,
// naming the exact kind; the effect dispatcher re-checks at execution time.
// An empty Kind is accepted because effect.Run / effect.Resolve treat it as
// noop. A loud, specific error at authoring time beats a deferred 500 from a
// handler that was never wired.
func ValidateAction(a Action) error {
	if a.Kind == "" {
		return nil
	}
	if !supportedActionKinds[a.Kind] {
		return fmt.Errorf("action kind %q is not supported by kiln/effect (supported: noop, validate, set_field, audit, respond_json, emit_event)", a.Kind)
	}
	return nil
}

// HookActionKinds are the kinds a hook can run: effect.RunHook dispatches
// through effect.Run, which performs a side effect and produces no
// response, so respond_json has nowhere to go on a hook.
var HookActionKinds = []string{ActionNoop, ActionValidate, ActionSetField, ActionAudit, ActionEmitEvent}

// RouteActionKinds are the kinds a route can run: render.applyRoutes
// answers every request through effect.Resolve, which only knows how to
// produce a response.
var RouteActionKinds = []string{ActionNoop, ActionRespondJSON}

// ValidateHookAction refuses a kind effect.Run cannot run. ValidateAction's
// one shared set admitted respond_json on a hook, which then failed every
// create, update, delete, or list it was attached to.
func ValidateHookAction(a Action) error {
	return validateSurfaceAction("hook", a, HookActionKinds)
}

// ValidateRouteAction refuses a kind effect.Resolve cannot answer with.
// The shared set admitted validate/set_field/audit/emit_event on a route,
// which then answered every request with a 500.
func ValidateRouteAction(a Action) error {
	return validateSurfaceAction("route", a, RouteActionKinds)
}

func validateSurfaceAction(surface string, a Action, kinds []string) error {
	if err := ValidateAction(a); err != nil {
		return err
	}
	if a.Kind == "" || slices.Contains(kinds, a.Kind) {
		return nil
	}
	return fmt.Errorf("action kind %q does not run on a %s (supported on a %s: %s)",
		a.Kind, surface, surface, strings.Join(kinds, ", "))
}

// validateNodeActions walks a UI node tree and rejects any unsupported action
// wired to an element's event handlers. A page can nest elements arbitrarily
// deep, so the walk is recursive.
func validateNodeActions(n *node.Node) error {
	if n == nil {
		return nil
	}
	for _, a := range n.Actions {
		if err := ValidateAction(a); err != nil {
			return err
		}
	}
	for i := range n.Children {
		if err := validateNodeActions(&n.Children[i]); err != nil {
			return err
		}
	}
	return nil
}

// ValidatePageActions walks a page's element tree and rejects any unsupported
// action wired to a node event handler.
func ValidatePageActions(p *Page) error {
	if p == nil {
		return nil
	}
	return validateNodeActions(&p.Tree)
}
