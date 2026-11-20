package app

import (
	"context"
	"errors"
	"net/http"
	"slices"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// RenderResult is the outcome of resolving and rendering a page or partial.
type RenderResult struct {
	Kind   DecisionKind
	HTML   render.HTML // populated for DecisionAllow and DecisionRenderAlt
	URL    string      // populated for DecisionRedirect
	Status int         // populated for DecisionBlock
	// Reason and Params are the DecisionBlock body, translated per request; a zero Reason means the
	// status's generic phrase.
	Reason i18n.Phrase
	Params map[string]any
	// Title is the screen's effective title, re-read from ScreenTitler on the per-request instance
	// AFTER Load, so dynamic routes (whose registration-time title is empty or generic) report the
	// loaded title.
	Title string
	// Component is the loaded per-request instance the HTML was rendered from (post SetParams → DI → Load).
	Component component.Component
	// SwapLayer is the layer key the partial HTML renders below when the render was a subtree partial
	// (RenderPartialFromResult with a known origin whose layout chain shares an addressable prefix with
	// the target's).
	SwapLayer string
	// Fills are the resolved non-primary fills of the KEPT layout layers (0..shared-1) of a subtree
	// partial whose chain holds tree layouts (NewLayout): each outlet and route area of a layer the
	// client keeps.
	Fills []Fill
	// FillFailures lists the outlet fills that failed and were contained during this render (variant
	// B): the HTML degraded each outlet to its fallback and the page marched on.
	FillFailures []FillFailure
	// Transition is the page's picked keyed-transition name the INNERMOST tree layer whose primary
	// declares a TransitionFor decides it.
	Transition string
	// NotFoundOutlet is the 404-outlet outcome (FallbackNotFound, Decided 5): an outlet of the route's
	// chain that no candidate fills made this render the not-found page — the primary is the app's
	// not-found body, every other outlet renders its Default or nothing, and Title is the not-found
	// body's.
	NotFoundOutlet bool
}

// FillFailure records one outlet fill that failed and was contained to its outlet.
type FillFailure struct {
	Addr string
	Err  error
}

// DecisionKind classifies the outcome of evaluating a Policy.
type DecisionKind int

const (
	// DecisionAllow lets the request proceed to normal Load+Render.
	DecisionAllow DecisionKind = iota
	// DecisionRedirect sends an HTTP 303 (or equivalent) to URL.
	DecisionRedirect
	// DecisionRenderAlt swaps Alt in for the screen's own component before Load+Render run.
	DecisionRenderAlt
	// DecisionBlock aborts with the given HTTP Status and optional Reason.
	DecisionBlock
)

// Decision is the outcome of Policy.Decide.
type Decision struct {
	Kind       DecisionKind
	URL        string                     // populated for DecisionRedirect
	AltFactory func() component.Component // populated for DecisionRenderAlt, called PER REQUEST
	Status     int                        // populated for DecisionBlock
	Reason     i18n.Phrase                // optional, for DecisionBlock
	Params     map[string]any             // Reason's params
	// NotFound marks a DecisionBlock that renders the not-found page an unmatched URL gets, instead of
	// the status phrase.
	NotFound bool
}

// Policy decides what to do with a request before screen render.
type Policy interface {
	Decide(ctx context.Context) Decision
}

// PolicyFunc adapts a plain function to the Policy interface.
type PolicyFunc func(ctx context.Context) Decision

// Decide implements Policy.
func (f PolicyFunc) Decide(ctx context.Context) Decision { return f(ctx) }

// EffectivePolicies returns the full policy chain for s: walk parent ScreenGroups
// outermost→innermost, then the screen's own policies.
func EffectivePolicies(s *Screen) []Policy {
	if s == nil {
		return nil
	}
	var chain []Policy
	if s.group != nil {
		var groups []*ScreenGroup
		for g := s.group; g != nil; g = g.parent {
			groups = append(groups, g)
		}
		for _, group := range slices.Backward(groups) {
			chain = append(chain, group.policies...)
		}
	}
	chain = append(chain, s.policies...)
	return chain
}

// PagePolicy runs the policy phase of a page render of path and returns its decision: the screen's
// policy chain, then the region guards, whose Redirect moves the whole page.
func (a *App) PagePolicy(ctx context.Context, path string) (d Decision, ok bool, err error) {
	screen, params, found := a.Router.Resolve(path)
	if !found {
		return Decision{}, false, nil
	}
	if _, has := MatchFromContext(ctx); !has {
		ctx = WithMatch(ctx, newMatch(screen.Path, path, params))
	}
	d, err = a.policyPhase(ctx, screen, path, params)
	return d, true, err
}

func (a *App) policyPhase(ctx context.Context, screen *Screen, path string, params map[string]string) (Decision, error) {
	var chain []LayoutLayer
	if screen.Type == ScreenPage {
		chain = resolveChainKeys(a.Router.layoutChainFor(screen), params)
	}
	_, o := runPolicyPhase(ctx, screen, path, chain)
	switch {
	case o.err != nil:
		return pageErrorDecision(o.err), o.err
	case o.redirect.Kind == DecisionRedirect:
		return o.redirect, nil
	}
	return o.decision, nil
}

type policyOutcome struct {
	decision Decision
	guards   regionGuards
	redirect Decision
	err      error
}

func runPolicyPhase(ctx context.Context, screen *Screen, path string, chain []LayoutLayer) (context.Context, policyOutcome) {
	ctx = withResolverState(ctx, screen)
	runEagerResolvers(ctx, screen)
	var o policyOutcome
	o.decision = ResolvePolicy(ctx, screen)
	if o.decision.Kind != DecisionRedirect && o.decision.Kind != DecisionBlock {
		o.guards, o.redirect = ensureRegionGuards(ctx, screen, chain)
	}
	o.err = resolverPageError(ctx, path)
	if o.err == nil && (o.decision.notFound() || o.redirect.notFound()) {
		o.err = &PageError{Path: path, Err: ErrNotFound, NotFound: true, Policy: true}
	}
	return ctx, o
}

func (d Decision) notFound() bool {
	return d.Kind == DecisionBlock && d.Status == http.StatusNotFound && d.NotFound
}

func pageErrorDecision(err error) Decision {
	if errors.Is(err, ErrNotFound) {
		return Decision{Kind: DecisionBlock, Status: http.StatusNotFound, NotFound: true}
	}
	return Decision{Kind: DecisionBlock, Status: http.StatusInternalServerError}
}

// PatternPolicy decides a document about screen's route pattern rather than one URL: the llm.md
// page index, a pattern doc, an action the client names no page for.
func (a *App) PatternPolicy(ctx context.Context, screen *Screen) Decision {
	if screen == nil {
		return Decision{Kind: DecisionBlock}
	}
	if len(extractParamNames(screen.Path)) == 0 {
		ctx = WithMatch(ctx, newMatch(screen.Path, screen.Path, nil))
		d, err := a.policyPhase(ctx, screen, screen.Path, nil)
		if err != nil && !d.notFound() {
			return Decision{Kind: DecisionBlock}
		}
		return d
	}
	d := ResolvePolicy(ctx, screen)
	if d.Kind != DecisionAllow {
		return d
	}
	if len(eagerDeclsFor(screen)) > 0 {
		return Decision{Kind: DecisionBlock}
	}
	if len(guardRegions(screen, a.Router.layoutChainFor(screen))) > 0 {
		return Decision{Kind: DecisionBlock}
	}
	return d
}

// ResolvePolicy walks the effective chain and returns the first non-Allow Decision.
func ResolvePolicy(ctx context.Context, s *Screen) Decision {
	for _, p := range EffectivePolicies(s) {
		d := p.Decide(ctx)
		if d.Kind != DecisionAllow {
			return d
		}
	}
	return Decision{Kind: DecisionAllow}
}
