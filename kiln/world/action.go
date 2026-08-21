package world

import (
	"fmt"

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
