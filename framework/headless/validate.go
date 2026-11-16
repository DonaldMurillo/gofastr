package headless

import "strings"

func hasControlBytes(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool { return r < 0x20 || r == 0x7f })
}

func checkSelector(component, what, sel string) {
	if sel == "" {
		panic("headless: " + component + " " + what + " is required — the observer has nothing to watch without it")
	}
	checkNoControlBytes(component, what, sel)
	if strings.Contains(sel, "<") {
		panic("headless: " + component + " " + what + " must be a selector, not markup: " + sel)
	}
}

func checkNoControlBytes(component, what, s string) {
	if hasControlBytes(s) {
		panic("headless: " + component + " " + what + " carries a control byte — values the browser reads may not: " + s)
	}
}

func checkLabel(component, what, label string) {
	if strings.TrimSpace(label) == "" {
		panic("headless: " + component + " " + what + " is required — a label of nothing but whitespace is a name nobody can read")
	}
}

func checkFragmentID(component, what, id string) {
	if id == "" {
		panic("headless: " + component + " " + what + " is required — a link to nothing is not a link")
	}
	checkNoControlBytes(component, what, id)
	if strings.ContainsAny(id, "<>\"' #") {
		panic("headless: " + component + " " + what + " must be a bare id, not a selector or markup: " + id)
	}
}

func checkStorageKey(component, key string) {
	if strings.TrimSpace(key) == "" {
		panic("headless: " + component + " persistence key is required when persistence is asked for — an empty key writes the whole origin's state")
	}
	checkNoControlBytes(component, "persistence key", key)
}

func checkEnum(component, what, value string, allowed ...string) {
	for _, a := range allowed {
		if value == a {
			return
		}
	}
	panic("headless: " + component + " " + what + " must be one of " + strings.Join(allowed, ", ") + ", not " + value)
}

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

func checkNoDuplicateIDs(component string, ids []string) {
	if d := duplicateIn(ids); d != "" {
		panic("headless: " + component + " renders the id " + d + " twice — every reference to it now points at the first one")
	}
}
