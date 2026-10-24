package analyzers_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1824: kit CSS that pins a physical side keeps it there under
// <html dir="rtl">, so the sidebar border, the list indent and the
// select chevron stay on the left-to-right side for an Arabic reader.
func TestPhysicalSidesAreReported(t *testing.T) {
	for _, tc := range []struct {
		css, want string
	}{
		{".a { border-right: var(--stroke-thin, 1px) solid var(--color-border); }", "border-inline-end"},
		{".a { padding-left: var(--spacing-lg, 16px); }", "padding-inline-start"},
		{".a { margin-right: auto; }", "margin-inline-end"},
		{".a { border-left-color: red; }", "border-inline-start-color"},
		{".a { left: var(--spacing-sm, 4px); }", "inset-inline-start"},
		{".a { right: 0; }", "inset-inline-end"},
		{".a { text-align: left; }", "text-align: start"},
		{".a { float: right; }", "float: inline-end"},
		{".a { border-top-left-radius: var(--radii-md, 8px); }", "border-start-start-radius"},
		{".a { background-position: right 12px center; }", ":dir(rtl)"},
		{".a { padding: var(--spacing-xs, 2px) var(--spacing-xs, 2px) var(--spacing-xs, 2px) var(--spacing-md, 8px); }", "padding-inline"},
		{".a { margin: 0 calc(var(--spacing-md, 8px) * -1) 0 0; }", "margin-inline"},
		{".a { border-radius: var(--radii-md, 8px) 0 0 var(--radii-md, 8px); }", "border-start-start"},
	} {
		ds := designFixture(t, "framework/ui/x.go", "package ui\n\nvar css = `"+tc.css+"`\n")
		found := countRule(t, ds, contracts.RulePhysicalSide)
		if len(found) != 1 {
			t.Errorf("%s: want 1 GOFASTR1824, got %d: %v", tc.css, len(found), found)
			continue
		}
		if !strings.Contains(found[0].Message, tc.want) {
			t.Errorf("%s: message does not name %s: %q", tc.css, tc.want, found[0].Message)
		}
	}
}

func TestPhysicalSideInSetPairIsReported(t *testing.T) {
	ds := designFixture(t, "core-ui/widget/page.go",
		"package widget\n\nfunc f(ss *Sheet) {\n\tss.Rule(\".a\").Set(\"left\", \"{spacing.lg}\", \"text-align\", \"right\").End()\n}\n")
	if found := countRule(t, ds, contracts.RulePhysicalSide); len(found) != 2 {
		t.Fatalf("want 2 GOFASTR1824 on the Set pairs, got %d: %v", len(found), found)
	}
}

// What stays legal: logical properties, centring at 50%, an auto
// reset, symmetric shorthands, a :dir() override, a background mirrored
// under :dir(rtl), Go code and keyword lists, dev tooling, and CSS
// outside the design-system trees.
func TestPhysicalSideQuietCases(t *testing.T) {
	for _, src := range []string{
		"var css = `.a { padding-inline-start: var(--spacing-lg, 16px); inset-inline-end: 0; text-align: start; }`",
		"var css = `.a { left: 50%; transform: translateX(-50%); }`",
		"var css = `.a { left: auto; inset-inline-start: calc(100% + 4px); }`",
		"var css = `.a { padding: var(--spacing-sm, 4px) var(--spacing-md, 8px); margin: 0 auto 0 auto; inset: 0 0 auto 0; }`",
		"var css = `.a:dir(rtl) { background-position: left 12px center; }`",
		"var css = `.a:dir(rtl) { margin-left: var(--spacing-sm, 4px); }`",
		"var css = `.a { background-position: right 12px center; }\n.a:dir(rtl) { background-position: left 12px center; }`",
		"func f(h *int) { left := *h; _ = left }",
		"var words = []string{\"join\", \"left\", \"right\", \"inner\"}",
		"var css = `.a { text-align: center; float: none; }`",
	} {
		ds := designFixture(t, "framework/ui/x.go", "package ui\n\n"+src+"\n")
		if found := countRule(t, ds, contracts.RulePhysicalSide); len(found) != 0 {
			t.Errorf("%s: want no GOFASTR1824, got %v", src, found)
		}
	}
	for rel, body := range map[string]string{
		"framework/dev/overlay.go": "package dev\n\nvar css = `.a { margin-left: auto; }`\n",
		"internal/app/page.go":     "package app\n\nvar css = `.a { margin-left: auto; }`\n",
	} {
		if found := countRule(t, designFixture(t, rel, body), contracts.RulePhysicalSide); len(found) != 0 {
			t.Errorf("%s: want no GOFASTR1824, got %v", rel, found)
		}
	}
}

// The allow marker is the one exception: a side that must stay
// physical says why.
func TestPhysicalSideAllowMarker(t *testing.T) {
	ds := designFixture(t, "framework/ui/x.go",
		"package ui\n\nvar css = `.a {\n` +\n\t//gofastr:allow(GOFASTR1824) the runtime measures this offset from the left edge\n\t`  left: var(--x, 0px);\n}`\n")
	if found := countRule(t, ds, contracts.RulePhysicalSide); len(found) != 0 {
		t.Fatalf("allow marker did not silence GOFASTR1824: %v", found)
	}
}

// A :dir() block spans lines: every declaration inside it is the
// override, the block's end hands the scan back, and a mirror counts
// only when it is CSS, not a comment that mentions one.
func TestPhysicalSideDirBlockAcrossLines(t *testing.T) {
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"var css = `.a:dir(rtl) {\n  margin-left: var(--spacing-sm, 4px);\n}`", 0},
		{"var css = `.a:dir(rtl) {\n  margin-left: 0;\n}\n.b {\n  margin-left: var(--spacing-sm, 4px);\n}`", 1},
		{"var css = `.a { background-position: right 12px center; }\n.a:dir(rtl) {\n  background-position: left 12px center;\n}`", 0},
		{"var css = `.a { background-position: right 12px center; }`\n\n// a :dir(rtl) rule could flip background-position here\n", 1},
	} {
		ds := designFixture(t, "framework/ui/x.go", "package ui\n\n"+tc.src+"\n")
		if found := countRule(t, ds, contracts.RulePhysicalSide); len(found) != tc.want {
			t.Errorf("%s: want %d GOFASTR1824, got %d: %v", tc.src, tc.want, len(found), found)
		}
	}
}
