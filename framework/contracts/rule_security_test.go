package contracts

import (
	"strings"
	"testing"
	"time"
)

// Property family: a rule reference resolves only when it spells the
// catalog entry exactly, ASCII case aside. Rule references are repo
// content — a suppression directive or a `rules:` key in
// gofastr.contracts.yml ships in the PR being verified — so a Unicode
// fold-equivalent that *resolves* is a review-evasion primitive: the
// directive reads like a typo ("gofaſtr1003") in the diff while it
// silently disables the real rule.

// Surfaces: LookupRule (the resolver), suppression directives
// (suppress.go add), and config rule keys (config.go applyRules) all
// share one resolver, so the property is asserted at each.
func TestLookupRuleRejectsFoldHomoglyphs(t *testing.T) {
	base, ok := LookupRule("GOFASTR1003")
	if !ok {
		t.Fatal("catalog rule GOFASTR1003 missing — update the fixture to another real rule")
	}
	cases := []struct {
		name string
		ref  string
		want bool
	}{
		{"exact ID", base.ID, true},
		{"lowercase ID", strings.ToLower(base.ID), true},
		{"long-s fold", strings.Replace(strings.ToLower(base.ID), "s", "ſ", 1), false},
		{"st ligature fold", strings.Replace(strings.ToLower(base.ID), "st", "ﬅ", 1), false},
		{"exact slug", base.Slug, true},
		{"long-s slug", strings.Replace(base.Slug, "s", "ſ", 1), false},
	}
	for _, c := range cases {
		if _, got := LookupRule(c.ref); got != c.want {
			t.Errorf("SECURITY: [homoglyph] LookupRule(%q) (%s) resolved=%v, want %v: a fold-equivalent spelling resolves %s, so a directive naming it suppresses the rule while reading like a typo in review",
				c.ref, c.name, got, c.want, base.ID)
		}
	}
}

func TestSuppressionHomoglyphRefStaysUnknown(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		"a.go": "package a\n\n//gofastr:allow(gofaſtr1003) reviewing upstream churn\nfunc f() {}\n",
	})
	if len(sup.byFile) != 0 {
		t.Errorf("SECURITY: [homoglyph] a directive naming gofaſtr1003 became a live suppression for GOFASTR1003: %+v", sup.byFile)
	}
	sawUnknown := false
	for _, d := range sup.issues {
		if d.RuleID == RuleSuppressionUnknownRule {
			sawUnknown = true
		}
	}
	if !sawUnknown {
		t.Error("SECURITY: [homoglyph] the homoglyph directive was not reported as naming an unknown rule — it resolved and suppressed silently")
	}
}

func TestConfigHomoglyphRuleKeyRejected(t *testing.T) {
	dir := t.TempDir()
	writeTemp(t, dir, "gofastr.contracts.yml", "contracts:\n  rules:\n    gofaſtr1003: off\n")
	if _, err := LoadConfig(dir, ""); err == nil {
		t.Error("SECURITY: [homoglyph] a rules key spelled with a Unicode fold-equivalent resolved and turned GOFASTR1003 off; it must be rejected like any other unknown rule")
	}
}

// SuggestRules is reachable with attacker-chosen needles: the
// unknown-rule branches in suppress.go and applyRules hand it the raw
// rule reference from a comment or YAML key in the verified repo. The
// fallback edit distance is O(needle × catalog) with no length cap, so
// a multi-megabyte reference makes one comment cost tens of seconds of
// CPU — a complexity DoS on `gofastr verify` over a hostile PR.
func TestSuggestRulesBoundedOnHugeNeedle(t *testing.T) {
	needle := strings.Repeat("x", 4<<20)
	start := time.Now()
	SuggestRules(needle)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("SECURITY: [complexity] SuggestRules on a %d-byte needle took %v; ranking cost must be bounded independent of needle length (cap the needle before edit distance)", len(needle), d)
	}
}
