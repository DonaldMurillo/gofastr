package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// ratingPreFix is ui.RatingInput's root rule before 85c52e6d: the radios
// rendered Max..1 and row-reverse painted them back as 1..Max, so the
// native radio arrows walked the stars backwards.
const ratingPreFix = "package ui\n\nfunc ratingCSS() string {\n\treturn `[data-cui-comp=\"ui-rating\"] {\n" +
	"  display: inline-flex;\n" +
	"  /* Flex-direction:row-reverse turns our reverse-DOM order back\n" +
	"     into 1..N visual order, while keeping the ~ sibling cascade. */\n" +
	"  flex-direction: row-reverse;\n" +
	"  justify-content: flex-end;\n" +
	"}`\n}\n"

// GOFASTR1826: a reversed flex container in kit CSS paints its children
// against the order a screen reader, Tab and a radio group's arrows use.
func TestFlexReverseIsReported(t *testing.T) {
	ds := designFixture(t, "framework/ui/rating.go", ratingPreFix)
	found := countRule(t, ds, contracts.RuleFlexReverse)
	if len(found) != 1 {
		t.Fatalf("want 1 GOFASTR1826 on the pre-fix Rating sheet (the comment above it is prose), got %d: %v", len(found), found)
	}
	if found[0].Line != 8 || !strings.Contains(found[0].Message, "row-reverse") {
		t.Fatalf("finding at line %d %q, want line 8 naming row-reverse", found[0].Line, found[0].Message)
	}
	for _, tc := range []struct{ rel, src string }{
		{"framework/ui/x.go", "var css = `.a { flex-direction: column-reverse; }`"},
		{"framework/ui/x.go", "var css = `.a { flex-flow: row-reverse wrap; }`"},
		{"framework/ui/x.go", "var css = `.a { FLEX-DIRECTION: Row-Reverse !important; }`"},
		{"core-ui/widget/page.go", "func f(ss *Sheet) {\n\tss.Rule(\".a\").Set(\"display\", \"flex\", \"flex-direction\", \"row-reverse\").End()\n}"},
		{"battery/admin/x.go", "var css = `.a{display:flex;flex-direction:row-reverse}`"},
	} {
		ds := designFixture(t, tc.rel, "package x\n\n"+tc.src+"\n")
		if found := countRule(t, ds, contracts.RuleFlexReverse); len(found) != 1 {
			t.Errorf("%s: want 1 GOFASTR1826, got %d: %v", tc.src, len(found), found)
		}
	}
}

// What stays legal: the forward spellings and the fix (forward order,
// :has() fill), wrap-reverse (it reverses the stacking of wrapped
// lines, not the order inside one), the keyword in prose, a comment or
// another property's value, dev tooling, and CSS outside the
// design-system trees.
func TestFlexReverseQuietCases(t *testing.T) {
	for _, src := range []string{
		"var css = `.a { display: inline-flex; flex-direction: row; }\n.a:has(~ .b:checked) { color: var(--ui-rating-color); }`",
		"var css = `.a { flex-flow: column wrap; flex-direction: column; }`",
		"var css = `.a { flex-wrap: wrap-reverse; flex-flow: row wrap-reverse; }`",
		"var css = `.a { /* flex-direction: row-reverse would break reading order */ display: flex; }`",
		"// flex-direction: row-reverse is refused by GOFASTR1826\nvar css = `.a { display: flex; }`",
		"var note = \"the old sheet used row-reverse\"",
		"var css = `.a { animation-direction: alternate-reverse; }`",
	} {
		ds := designFixture(t, "framework/ui/x.go", "package ui\n\n"+src+"\n")
		if found := countRule(t, ds, contracts.RuleFlexReverse); len(found) != 0 {
			t.Errorf("%s: want no GOFASTR1826, got %v", src, found)
		}
	}
	for rel, body := range map[string]string{
		"framework/dev/overlay.go": "package dev\n\nvar css = `.a { flex-direction: row-reverse; }`\n",
		"internal/app/page.go":     "package app\n\nvar css = `.a { flex-direction: row-reverse; }`\n",
	} {
		if found := countRule(t, designFixture(t, rel, body), contracts.RuleFlexReverse); len(found) != 0 {
			t.Errorf("%s: want no GOFASTR1826, got %v", rel, found)
		}
	}
}

// The allow marker is the one exception: a reversed box whose children
// carry no order of their own says why.
func TestFlexReverseAllowMarker(t *testing.T) {
	ds := designFixture(t, "framework/ui/x.go",
		"package ui\n\nvar css = `.a {\n` +\n\t//gofastr:allow(GOFASTR1826) a decorative stack of identical dots with no reading order\n\t`  flex-direction: row-reverse;\n}`\n")
	if found := countRule(t, ds, contracts.RuleFlexReverse); len(found) != 0 {
		t.Fatalf("allow marker did not silence GOFASTR1826: %v", found)
	}
}
