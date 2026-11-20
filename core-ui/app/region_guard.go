package app

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
)

// FillOption configures one fill declaration (Screen.Fill, ScreenGroup.Fill).
type FillOption func(*fillDecl)

// FillPolicy attaches a guard to the fill being declared: it runs in the policy phase, before any
// Load, beside the outlet's own spec policy (the spec first).
func FillPolicy(p Policy) FillOption {
	return func(d *fillDecl) { d.policy = p }
}

type regionDecision struct {
	kind    DecisionKind
	factory func() component.Component
}

type guardedRegion struct {
	addr     string
	slotName string // non-empty for outlet regions
	policies []Policy
}

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

func fillHasPolicy(screen *Screen, o *Outlet) bool {
	return len(fillPoliciesFor(screen, o)) > 0
}

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
				if d.notFound() && !screen.notFoundOutcome && !screen.errorOutcome {
					return nil, d
				}
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

type regionGuards = map[string]regionDecision

func altCandidates(guards regionGuards, slot fillSlot) []component.Component {
	d, ok := guards[slot.addr]
	if !ok || d.kind != DecisionRenderAlt || d.factory == nil {
		return nil
	}
	return append([]component.Component{d.factory()}, slot.candidates...)
}

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
