package runtime

import (
	"sort"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/runtime/internal/treegate"
)

// runtimeJSAttrs is the attribute set the fragment map below must own.
func runtimeJSAttrs(t *testing.T) []string {
	t.Helper()
	attrs, err := treegate.RuntimeJSAttrs(".")
	if err != nil {
		t.Fatal(err)
	}
	return attrs
}

// --- Runtime composition gate (spec: scratchpad/SPEC-runtime-composer.md) ---
//
// The tests below extend the attribute parity gates in internal/treegate
// to the attribute→fragment map declared in fragments.go. Together they enforce
// that every data-cui-* attribute in the runtime sources has exactly one
// declared owner (a core fragment or an on-demand module), that the
// declared set never drifts ahead of the source, and that the fragment
// dependency graph is well-formed. This is the build-time half of Hard
// rule 5: a new attribute without an owning fragment fails the build
// before it can ship, and a removed attribute leaves a stale entry that
// also fails.
//
// They read treegate.RuntimeJSAttrs, the same source-of-truth scanner the
// doc-parity gates use, so there is one definition of "what attributes
// exist", not two.

// TestFragmentMapComplete is gate item 1: every data-cui-* attribute the
// runtime JS references must have a declared owning fragment or module in
// fragments.go. A new attribute that ships without an entry here fails the
// build, which is the point: composition cannot serve an attribute whose
// owner it does not know, and the map is how it knows.
func TestFragmentMapComplete(t *testing.T) {
	var unowned []string
	for _, a := range runtimeJSAttrs(t) {
		kind, _ := attrOwner(a)
		if kind == "" {
			unowned = append(unowned, a)
		}
	}
	if len(unowned) > 0 {
		t.Errorf("%d data-cui-* attribute(s) referenced by the runtime sources have "+
			"no owning fragment or module in fragments.go (runtime-composer gate):\n  %s\n"+
			"Assign each to the fragment (fragmentAttrs) or src/*.js module (moduleAttrs) "+
			"that implements its handler — see the ownership rule in fragments.go.",
			len(unowned), strings.Join(unowned, "\n  "))
	}
}

// TestFragmentMapNoStale is gate item 2: every attribute declared in
// fragments.go must still exist in the runtime sources. A stale entry is
// usually a rename or removal that forgot to update the map; left in place
// it would make the composition serve a fragment for a marker nobody emits.
func TestFragmentMapNoStale(t *testing.T) {
	src := map[string]struct{}{}
	for _, a := range runtimeJSAttrs(t) {
		src[a] = struct{}{}
	}
	check := func(owner, attr string) {
		if _, ok := src[attr]; !ok {
			t.Errorf("attribute %q declared under %q in fragments.go is NOT present in any "+
				"runtime source (runtime.js or src/*.js) — it was likely renamed or removed; "+
				"drop the entry or reassign the new name.", attr, owner)
		}
	}
	for frag, attrs := range fragmentAttrs {
		for _, a := range attrs {
			check("fragment "+frag, a)
		}
	}
	for mod, attrs := range moduleAttrs {
		for _, a := range attrs {
			check("module "+mod, a)
		}
	}
}

// TestFragmentMapNoDuplicate asserts the two tables never assign the same
// attribute twice. A double-assignment would make the completeness gate
// ambiguous about which owner serves the marker, and is almost always a
// transcription error introduced while splitting the map.
func TestFragmentMapNoDuplicate(t *testing.T) {
	seen := make(map[string]string) // attr -> first owner
	record := func(owner, attr string) {
		if prev, ok := seen[attr]; ok {
			t.Errorf("attribute %q is assigned to BOTH %q and %q — each data-cui-* attribute "+
				"has exactly one owner; pick the fragment/module that implements its handler.",
				attr, prev, owner)
		}
		seen[attr] = owner
	}
	for frag, attrs := range fragmentAttrs {
		for _, a := range attrs {
			record("fragment "+frag, a)
		}
	}
	for mod, attrs := range moduleAttrs {
		for _, a := range attrs {
			record("module "+mod, a)
		}
	}
}

// TestFragmentNamesValid is gate item 3: every key in fragmentAttrs must be
// one of the declared fragment names in `fragments`. A typo here
// (e.g. "widget-boot" vs "widgets-boot") would otherwise silently produce a
// fragment that no composition can name.
func TestFragmentNamesValid(t *testing.T) {
	for frag := range fragmentAttrs {
		if _, ok := fragments[frag]; !ok {
			t.Errorf("fragmentAttrs key %q is not in the declared fragment set `fragments` "+
				"(valid names: kernel, signals, nav, widgets-boot, sse, compute, boot-embed) "+
				"— fix the typo or add the fragment to `fragments`.", frag)
		}
	}
}

// TestFragmentDepsAcyclic is gate item 4: the declared dependency edges
// must form a DAG and every named dependency must resolve to a declared
// fragment. Without this the composer's closure could recurse infinitely or
// reference a fragment that does not exist, which is the failure mode that
// turns the map from a gate into mere documentation.
func TestFragmentDepsAcyclic(t *testing.T) {
	const (
		white = 0 // unvisited
		gray  = 1 // on the current DFS path (back-edge ⇒ cycle)
		black = 2 // fully explored
	)
	color := make(map[string]int, len(fragments))
	var visit func(name string, path []string) bool
	visit = func(name string, path []string) bool {
		switch color[name] {
		case gray:
			t.Errorf("fragment dependency cycle: %s -> %s",
				strings.Join(append(path, name), " -> "), name)
			return false
		case black:
			return true
		}
		def, ok := fragments[name]
		if !ok {
			t.Errorf("dependency %q is referenced in a fragment's deps but is not a declared "+
				"fragment — add it to `fragments` or fix the name.", name)
			return false
		}
		color[name] = gray
		for _, d := range def.deps {
			if !visit(d, append(path, name)) {
				return false
			}
		}
		color[name] = black
		return true
	}
	// Visit in sorted order so any failure report is stable across runs.
	names := make([]string, 0, len(fragments))
	for n := range fragments {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		visit(n, nil)
	}
}

// TestFragmentModulesValid extends gate item 3 to module-owned attributes:
// every key in moduleAttrs must be a real embedded runtime module (a file
// under src/*.js served at /__gofastr/runtime/<name>.js). A module name that
// no .js file backs would direct a marker's demand-load at a 404. Named
// TestFragment* so every gate test runs under a single -run filter.
func TestFragmentModulesValid(t *testing.T) {
	real := map[string]struct{}{}
	for _, n := range ModuleNames() {
		real[n] = struct{}{}
	}
	var stale []string
	for mod := range moduleAttrs {
		if _, ok := real[mod]; !ok {
			stale = append(stale, mod)
		}
	}
	if len(stale) > 0 {
		sort.Strings(stale)
		t.Errorf("%d moduleAttrs key(s) are not embedded src/*.js modules:\n  %s\n"+
			"Every moduleAttrs key must match a src/<name>.js file. Drop the entry or "+
			"add the module file.", len(stale), strings.Join(stale, "\n  "))
	}
}
