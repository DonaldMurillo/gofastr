package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// RenderResult is the outcome of resolving and rendering a page or
// partial. HTTP hosts inspect Kind first to choose between writing the
// HTML body, issuing a redirect, or returning an error status.
type RenderResult struct {
	Kind    DecisionKind
	HTML    render.HTML // populated for DecisionAllow and DecisionRenderAlt
	URL     string      // populated for DecisionRedirect
	Status  int         // populated for DecisionBlock
	Message string      // optional, for DecisionBlock
	// Title is the screen's effective title, re-read from ScreenTitler on
	// the per-request instance AFTER Load, so dynamic routes (whose
	// registration-time title is empty or generic) report the loaded
	// title. Empty when the component doesn't implement ScreenTitler.
	// Hosts should prefer it over the registration-time Screen.Title.
	Title string
	// Component is the loaded per-request instance the HTML was rendered
	// from (post SetParams → DI → Load). Host layers use it to resolve
	// per-instance metadata — SEO description, og tags — that the shared
	// registration instance cannot know for dynamic routes. Read-only;
	// nil for Redirect/Block results.
	Component component.Component
}

// DecisionKind classifies the outcome of evaluating a Policy.
type DecisionKind int

const (
	// DecisionAllow lets the request proceed to normal Load+Render.
	DecisionAllow DecisionKind = iota
	// DecisionRedirect sends an HTTP 303 (or equivalent) to URL.
	DecisionRedirect
	// DecisionRenderAlt swaps Alt in for the screen's own component
	// before Load+Render run. The original screen's Load is skipped;
	// Alt's Load runs if Alt implements ScreenLoader.
	DecisionRenderAlt
	// DecisionBlock aborts with the given HTTP Status and optional
	// Message. Use for hard 401/403/404 outcomes.
	DecisionBlock
)

// Decision is the outcome of Policy.Decide. Construct with the helper
// functions (Allow, Redirect, RenderAlt, Block) — the struct shape is
// public only so hosts can inspect it.
type Decision struct {
	Kind       DecisionKind
	URL        string                     // populated for DecisionRedirect
	AltFactory func() component.Component // populated for DecisionRenderAlt — called PER REQUEST
	Status     int                        // populated for DecisionBlock
	Message    string                     // optional, for DecisionBlock
}

// Decision values are constructed via the helpers in subpackage
// core-ui/app/decide (decide.Allow, decide.Redirect, decide.RenderAlt,
// decide.Block). The subpackage exists to avoid shadowing common
// variable names like `redirect` and `block` at call sites.

// Policy decides what to do with a request before screen render.
// Implementations must be stateless and side-effect-free; the resolver
// may call Decide multiple times during a request and never holds
// onto the returned Decision past the response.
type Policy interface {
	Decide(ctx context.Context) Decision
}

// PolicyFunc adapts a plain function to the Policy interface.
type PolicyFunc func(ctx context.Context) Decision

// Decide implements Policy.
func (f PolicyFunc) Decide(ctx context.Context) Decision { return f(ctx) }

// EffectivePolicies returns the full policy chain for s: walk parent
// ScreenGroups outermost→innermost, then the screen's own policies.
// Hosts rarely need to call this directly — use ResolvePolicy.
func EffectivePolicies(s *Screen) []Policy {
	if s == nil {
		return nil
	}
	var chain []Policy
	if s.group != nil {
		// Collect groups inner→outer first, then reverse for outer→inner.
		var groups []*ScreenGroup
		for g := s.group; g != nil; g = g.parent {
			groups = append(groups, g)
		}
		for i := len(groups) - 1; i >= 0; i-- {
			chain = append(chain, groups[i].policies...)
		}
	}
	chain = append(chain, s.policies...)
	return chain
}

// ResolvePolicy walks the effective chain and returns the first
// non-Allow Decision. Returns Allow when the chain is empty or every
// policy permits the request.
func ResolvePolicy(ctx context.Context, s *Screen) Decision {
	for _, p := range EffectivePolicies(s) {
		d := p.Decide(ctx)
		if d.Kind != DecisionAllow {
			return d
		}
	}
	return Decision{Kind: DecisionAllow}
}
