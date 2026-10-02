package app

// transitions picked from the route
// resolution (docs/DESIGN-layout-outlets.md, "Settings from the
// resolution"). A primary or outlet slot may declare a SET of
// transitions keyed by name (Transitions) plus a per-request picker
// (TransitionFor, reading resolvers or the match); the rendered cell
// carries the picked name, the stylesheet is generated ONCE in sorted
// key order (static, CSP-safe), and the page answer carries the pick
// in X-Gofastr-Transition so the runtime can add it to the
// view-transition types beside the direction.

import (
	"context"
	"sort"
)

// directionVTNames are the view-transition types the runtime owns
// (forward/back/reload); a keyed transition may not reuse them.
var directionVTNames = map[string]bool{"forward": true, "back": true, "reload": true}

// keyedPick resolves a slot's per-request transition: TransitionFor's
// answer names an entry of the Transitions set. Empty or unknown names
// pick nothing (the runtime's vocabulary rule, server-side: a pick the
// set does not declare is ignored, never an error at render time — the
// mount check already refused the direction names).
func keyedPick(ctx context.Context, tf func(context.Context) string, set map[string]Transition) (string, Transition) {
	if tf == nil || len(set) == 0 {
		return "", Transition{}
	}
	name := tf(ctx)
	if name == "" {
		return "", Transition{}
	}
	tr, ok := set[name]
	if !ok {
		return "", Transition{}
	}
	return name, tr
}

// keyedSlotName is the slot name a keyed transition's generated
// view-transition-name derives from: the slot plus the pick, so two
// picks of one slot never share a name (two live names of one spelling
// make the browser skip the whole transition).
func keyedSlotName(slot, pick string) string {
	if pick == "" {
		return slot
	}
	return slot + "-" + pick
}

// sortedVTKeys lists a transition set's keys in sorted order — the
// CSS generation order and the kinds-vocabulary order alike.
func sortedVTKeys(set map[string]Transition) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// chainTransitionPick returns the page's pick: the INNERMOST tree
// layer whose primary declares a TransitionFor decides it (an inner
// layer's pick overrides an outer's), validated against its own set.
func chainTransitionPick(ctx context.Context, chain []LayoutLayer) string {
	for i := len(chain) - 1; i >= 0; i-- {
		l := chain[i].Layout
		if l == nil || !l.isTree() || l.spec == nil || l.spec.Primary.TransitionFor == nil {
			continue
		}
		pick, _ := keyedPick(ctx, l.spec.Primary.TransitionFor, l.spec.Primary.Transitions)
		return pick
	}
	return ""
}

// chainVTKinds lists the document's declared transition vocabulary:
// every keyed transition name of the chain's tree layers (primaries
// and outlets), sorted. It rides the doc shell as data-fui-vt-kinds so
// the runtime can ignore a pick the document never declared.
func chainVTKinds(chain []LayoutLayer) []string {
	seen := map[string]bool{}
	var out []string
	add := func(set map[string]Transition) {
		for k := range set {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	for _, layer := range chain {
		l := layer.Layout
		if l == nil || !l.isTree() || l.spec == nil {
			continue
		}
		add(l.spec.Primary.Transitions)
		for _, o := range l.spec.Outlets {
			add(o.Transitions)
		}
	}
	sort.Strings(out)
	return out
}
