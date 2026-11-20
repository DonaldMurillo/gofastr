package app

import (
	"context"
	"sort"
)

var directionVTNames = map[string]bool{"forward": true, "back": true, "reload": true}

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

func keyedSlotName(slot, pick string) string {
	if pick == "" {
		return slot
	}
	return slot + "-" + pick
}

func sortedVTKeys(set map[string]Transition) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

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
