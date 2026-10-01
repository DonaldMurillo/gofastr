package app

// region guards — a Policy attached
// to an outlet (OutletSpec.Policy), a route area (AreaSpec.Policy), or
// one fill declaration (app.FillPolicy on Screen.Fill /
// ScreenGroup.Fill). Every region guard runs in the POLICY phase,
// before any Load, in declaration order after the screen chain; the
// first Redirect moves the whole page and no Load runs. RenderAlt and
// Block are recorded per region and applied when that region resolves
// (Block renders the region's fallback; RenderAlt the alt component in
// place of the fill candidates / the area's fn).

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// FillOption configures one fill declaration (Screen.Fill,
// ScreenGroup.Fill).
type FillOption func(*fillDecl)

// FillPolicy attaches a guard to the fill being declared: it runs in
// the policy phase, before any Load, beside the outlet's own spec
// policy (the spec first). A Redirect decision moves the whole page;
// RenderAlt replaces THIS fill's candidates with the alt component;
// Block renders the outlet's fallback (the Default, else nothing).
func FillPolicy(p Policy) FillOption {
	return func(d *fillDecl) { d.policy = p }
}

// regionDecision is one region's recorded guard outcome: the first
// non-Allow decision among its guards.
type regionDecision struct {
	kind    DecisionKind
	factory func() component.Component
}

// guardedRegion is one region and its ordered guards.
type guardedRegion struct {
	addr     string
	slotName string // non-empty for outlet regions
	policies []Policy
}

// guardRegions lists every guard-bearing region of the screen's chain,
// in declaration order, with the policies that own it. Order: chain
// order; within a layer, outlets in spec order then areas in spec
// order; for one outlet, the spec's policy, then the screen fill's,
// then group fills innermost → outermost.
func guardRegions(screen *Screen, chain []LayoutLayer) []guardedRegion {
	var out []guardedRegion
	for _, layer := range chain {
		if layer.Layout == nil || !layer.Layout.isTree() {
			continue
		}
		spec := layer.Layout.spec
		if spec == nil {
			continue
		}
		key := layer.Key()
		for _, o := range spec.Outlets {
			if o.Policy == nil && !fillHasPolicy(screen, o) {
				continue
			}
			gr := guardedRegion{addr: key + "#" + o.Name(), slotName: o.Name()}
			if o.Policy != nil {
				gr.policies = append(gr.policies, o.Policy)
			}
			gr.policies = append(gr.policies, fillPoliciesFor(screen, o)...)
			out = append(out, gr)
		}
		for i := range spec.Areas {
			a := &spec.Areas[i]
			if a.Policy == nil {
				continue
			}
			out = append(out, guardedRegion{addr: key + "~" + a.Name, policies: []Policy{a.Policy}})
		}
	}
	return out
}

// fillHasPolicy reports whether any fill declaration targeting this
// layout+outlet carries a policy.
func fillHasPolicy(screen *Screen, o *Outlet) bool {
	return len(fillPoliciesFor(screen, o)) > 0
}

// fillPoliciesFor lists the fill-declared policies for one outlet, in
// consultation order: the screen's own fill, then group fills
// innermost → outermost.
func fillPoliciesFor(screen *Screen, o *Outlet) []Policy {
	var out []Policy
	if screen != nil {
		if d, ok := screen.fills[o]; ok && d.policy != nil {
			out = append(out, d.policy)
		}
	}
	for g := screen.group; g != nil; g = g.parent {
		if d, ok := g.fills[o]; ok && d.policy != nil {
			out = append(out, d.policy)
		}
	}
	return out
}

// runRegionGuards evaluates every region guard in the policy phase.
// The first Redirect is returned as a whole-page decision (no Load
// runs); every other non-Allow decision is recorded per region in the
// returned map, keyed by wire address, and applied when that region
// resolves. Guards read resolvers in the policy phase, so a failing
// read is a whole-page outcome exactly like a screen policy's.
func runRegionGuards(ctx context.Context, screen *Screen, chain []LayoutLayer) (map[string]regionDecision, Decision) {
	regions := guardRegions(screen, chain)
	if len(regions) == 0 {
		return nil, Decision{}
	}
	guards := make(map[string]regionDecision, len(regions))
	for _, gr := range regions {
		for _, p := range gr.policies {
			d := p.Decide(ctx)
			switch d.Kind {
			case DecisionAllow:
				continue
			case DecisionRedirect:
				return nil, d
			case DecisionRenderAlt, DecisionBlock:
				rd := regionDecision{kind: d.Kind}
				if d.Kind == DecisionRenderAlt {
					rd.factory = d.AltFactory
				}
				guards[gr.addr] = rd
			}
			break // the region's first non-Allow decision wins
		}
	}
	return guards, Decision{}
}

// regionGuards is a render's recorded region decisions, keyed by wire
// address; resolveFills consults them per slot, RouteArea per area.
// nil (no guards) renders everything as declared.
type regionGuards = map[string]regionDecision

// altCandidates returns slot's candidate list with a RenderAlt guard's
// factory component PREPENDED (the alt outranks the screen fill), or
// nil when the region recorded no RenderAlt.
func altCandidates(guards regionGuards, slot fillSlot) []component.Component {
	d, ok := guards[slot.addr]
	if !ok || d.kind != DecisionRenderAlt || d.factory == nil {
		return nil
	}
	return append([]component.Component{d.factory()}, slot.candidates...)
}

// ensureRegionGuards computes the request's region decisions once and
// caches them on the resolver store: the first caller (the policy
// phase of the base render) runs the guards; later stages of the same
// request (the subtree partial's fills pass, a part request's own
// ladder) read the recorded map. The returned Decision is a Redirect
// when any guard redirected — the whole page moves and no Load runs.
func ensureRegionGuards(ctx context.Context, screen *Screen, chain []LayoutLayer) (regionGuards, Decision) {
	st, ok := ctx.Value(resolverContextKey{}).(*resolverState)
	if !ok {
		return nil, Decision{}
	}
	if st.store.guardsDone {
		return st.store.guards, st.store.guardRedirect
	}
	guards, d := runRegionGuards(ctx, screen, chain)
	st.store.guards = guards
	st.store.guardRedirect = d
	st.store.guardsDone = true
	return guards, d
}
