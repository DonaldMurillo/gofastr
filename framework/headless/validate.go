package headless

import "strings"

// The refusal helpers the navigation primitives share. A refusal is
// for configuration — a selector, an id, a key, an enum value the
// caller typed — and it fires at render, where the mistake is a panic
// with a reason. Data a request carried (a query, a typed value) is
// never refused here: it is scrubbed with scrubControlBytes or clamped
// and rendered, so a hostile POST cannot take a page down.

// hasControlBytes reports whether s carries a C0 control byte or DEL.
// The same walk scrubControlBytes does, as a question.
func hasControlBytes(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

// checkSelector refuses a CSS selector the primitives hand to the
// browser: one that is empty when a value is required, carries a
// control byte, or opens an element. Everything else a selector can
// get wrong the module turns into a safe no-op — querySelector throws
// on malformed input and the module catches it — so the refusal stops
// at what would not even reach the browser intact.
func checkSelector(component, what, sel string) {
	if sel == "" {
		panic("headless: " + component + " " + what + " is required — the observer has nothing to watch without it")
	}
	checkNoControlBytes(component, what, sel)
	if strings.Contains(sel, "<") {
		panic("headless: " + component + " " + what + " must be a selector, not markup: " + sel)
	}
}

// checkNoControlBytes refuses a value that cannot travel into an
// attribute or a selector intact. A CR in a storage key or an
// aria-label is not a value a reader or a browser can use, and the
// same byte smuggled past a sanitizer is how attribute boundaries
// blur.
func checkNoControlBytes(component, what, s string) {
	if hasControlBytes(s) {
		panic("headless: " + component + " " + what + " carries a control byte — values the browser reads may not: " + s)
	}
}

// checkLabel refuses a required label that is nothing but whitespace.
// An empty string is caught by a caller's own zero check half the
// time, but "   " sails through every one of those and renders a
// landmark, a control or a link whose accessible name collapses to
// nothing once the browser folds the space — the same namelessness
// the empty check exists to stop. Control bytes stay outside this
// check on purpose: the render path scrubs those (scrubControlBytes),
// the package's split between configuration refusals and data
// scrubbing, and a label that reaches here with one is data a request
// carried, not a mistake at the call site.
func checkLabel(component, what, label string) {
	if strings.TrimSpace(label) == "" {
		panic("headless: " + component + " " + what + " is required — a label of nothing but whitespace is a name nobody can read")
	}
}

// checkFragmentID refuses an in-page fragment id the primitives link
// to. The id becomes half of href="#<id>" and half of the target
// element's id; a control byte or markup opener in it breaks the pair
// before the browser resolves it.
func checkFragmentID(component, what, id string) {
	if id == "" {
		panic("headless: " + component + " " + what + " is required — a link to nothing is not a link")
	}
	checkNoControlBytes(component, what, id)
	if strings.ContainsAny(id, "<>\"' #") {
		panic("headless: " + component + " " + what + " must be a bare id, not a selector or markup: " + id)
	}
}

// checkStorageKey refuses a key the modules persist state under. The
// storage namespaces are load-bearing (a key that collides with
// another component's writes clobbers it), so an empty key where one
// is required, a control byte, or whitespace-only filler is a mistake
// at the call site, not a value to repair.
func checkStorageKey(component, key string) {
	if strings.TrimSpace(key) == "" {
		panic("headless: " + component + " persistence key is required when persistence is asked for — an empty key writes the whole origin's state")
	}
	checkNoControlBytes(component, "persistence key", key)
}

// checkEnum refuses a typed-string value outside the set the
// component renders. The enum's constants are the API; a value outside
// them has no class, no behaviour and no contract, so it is refused
// here rather than rendered as an unknown variant.
func checkEnum(component, what, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	panic("headless: " + component + " " + what + " must be one of " + strings.Join(allowed, ", ") + ", not " + value)
}

// duplicateIn reports the first value seen twice in values, or "".
func duplicateIn(values []string) string {
	seen := make(map[string]bool, len(values))
	for _, v := range values {
		if seen[v] {
			return v
		}
		seen[v] = true
	}
	return ""
}

// checkNoDuplicateIDs refuses a component whose collected ids name one
// element twice: every reference to the duplicated id — an
// aria-controls, a fragment href, a label's for — now points at the
// first one and silently detaches from the second.
func checkNoDuplicateIDs(component string, ids []string) {
	if d := duplicateIn(ids); d != "" {
		panic("headless: " + component + " renders the id " + d + " twice — every reference to it now points at the first one")
	}
}
