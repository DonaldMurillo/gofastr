package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1804 shares its detector with `cmd/check-csp`
// (core-ui/check): two implementations of one check is how the rule
// came to miss the most common shape of inline style — a keyword or
// unitless value (`display: none`, `color: red`, `flex: 1`) — while
// the build gate caught it.

// A keyword or unitless value is a real declaration: the CSP strips
// `style="display: none"` exactly as it strips `style="margin: 4px"`.
func TestInlineStyleKeywordValueIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"ui.go": "package main\n" +
			"var a = `<div style=\"display: none\">x</div>`\n" +
			"var b = `<div style=\"color: red\">y</div>`\n" +
			"var c = `<div style=\"flex: 1\">z</div>`\n" +
			"var d = `<div style=\"overflow: hidden\">w</div>`\n",
	})
	if got := countRule(t, ds, contracts.RuleInlineStyle); len(got) != 4 {
		t.Fatalf("want 4 findings (display, color, flex, overflow), got %d: %v", len(got), got)
	}
}

// The computed spellings the string scan cannot see: a "style" key in
// an attrs map and an index assignment onto one. check-csp flags both;
// verify must agree.
func TestInlineStyleComputedSpellingsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"ui.go": "package main\n" +
			"var m = map[string]string{\"style\": \"display:none\"}\n" +
			"func f(attrs map[string]string) { attrs[\"style\"] = \"flex:1\" }\n",
	})
	if got := countRule(t, ds, contracts.RuleInlineStyle); len(got) != 2 {
		t.Fatalf("want 2 findings (map key, index assignment), got %d: %v", len(got), got)
	}
}
