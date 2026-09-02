// Package af holds the asciifold fixtures. The positives are reduced
// from framework/contracts/rule.go at 7bd789e9 (fixed in 77fdbaf4),
// keeping the real names, with the fixed spelling as the negative.
package af

import "strings"

// rule mirrors contracts.Rule: a registry entry, not a plain value.
type rule struct {
	ID         string
	Capability string
}

var rulesByID = map[string]rule{
	"GOFASTR1003": {ID: "GOFASTR1003", Capability: "runtime"},
}

var rulesBySlug = map[string]string{
	"ascii-fold": "GOFASTR1003",
}

// lookupRuleLocked is the pre-fix shape: the ID registry (struct values)
// is keyed by ToUpper, so "gofaſtr1003" resolves GOFASTR1003
// (TestLookupRuleRejectsFoldHomoglyphs).
func lookupRuleLocked(idOrSlug string) (rule, bool) {
	key := strings.TrimSpace(idOrSlug)
	if r, ok := rulesByID[strings.ToUpper(key)]; ok { // want `registry lookup folds Unicode case`
		return r, true
	}
	// The slug map holds plain strings: nothing to impersonate, silent.
	if id, ok := rulesBySlug[strings.ToLower(key)]; ok {
		return rulesByID[id], true
	}
	return rule{}, false
}

// lookupRuleFixed is the fix's shape: non-ASCII keys are refused before
// any folding, so ToUpper can no longer fold a homoglyph onto an entry.
func lookupRuleFixed(idOrSlug string) (rule, bool) {
	key := strings.TrimSpace(idOrSlug)
	if strings.ContainsFunc(key, func(r rune) bool { return r >= 0x80 }) {
		return rule{}, false
	}
	if r, ok := rulesByID[strings.ToUpper(key)]; ok {
		return r, true
	}
	if id, ok := rulesBySlug[strings.ToLower(key)]; ok {
		return rulesByID[id], true
	}
	return rule{}, false
}

// suggestRules folds for substring ranking, never for an index: silent.
func suggestRules(idOrSlug string) []string {
	needle := strings.ToLower(idOrSlug)
	var out []string
	for id := range rulesByID {
		if strings.Contains(strings.ToLower(id), needle) {
			out = append(out, id)
		}
	}
	return out
}
