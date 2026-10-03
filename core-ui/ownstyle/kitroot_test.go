package ownstyle

import (
	"slices"
	"testing"
)

func kitRootLines(t *testing.T, css string, roots ...KitRoot) []int {
	t.Helper()
	sheet, diags := Parse(css)
	if len(diags) > 0 {
		t.Fatalf("parse: %v", diags)
	}
	var lines []int
	for _, d := range CheckKitRoots(sheet, roots) {
		if d.Rule != RuleKitRootStyle || d.Severity != SeverityError {
			t.Errorf("unexpected diagnostic %v", d)
		}
		lines = append(lines, d.Line)
	}
	return lines
}

func TestKitRootsSubjectOnly(t *testing.T) {
	css := ".column { margin: 0; padding: 1px; }\n" + // 1: padding
		".column .count { color: red; }\n" + // 2: subject is .count
		".column:hover { color: red; }\n" + // 3: base .column
		".lane { color: red; }\n" + // 4: not a root
		"@media (--above-md) { .column { background: red; } }\n" + // 5
		"@keyframes spin { to { rotate: 1turn; } }\n" + // 6: not a selector
		".column { & .x { color: red; } &:focus { outline: 0; } }\n" // 7: only &:focus
	got := kitRootLines(t, css, KitRoot{Class: "column", Via: "test"})
	if want := []int{1, 3, 5, 7}; !slices.Equal(got, want) {
		t.Errorf("lines = %v, want %v", got, want)
	}
}

func TestKitRootsScope(t *testing.T) {
	css := ":scope { display: grid; color: red; }\n.column { color: red; }\n"
	if got := kitRootLines(t, css, KitRoot{Scope: true, Via: "test"}); !slices.Equal(got, []int{1}) {
		t.Errorf("lines = %v, want [1]", got)
	}
	if got := kitRootLines(t, css); len(got) != 0 {
		t.Errorf("no roots, want no findings; got %v", got)
	}
}

func TestPlacementPropertyList(t *testing.T) {
	for _, p := range []string{"grid-area", "grid-column-start", "margin-inline", "min-width", "max-block-size", "inset-inline", "z-index", "display", "Order"} {
		if !IsPlacementProperty(p) {
			t.Errorf("%s should be placement", p)
		}
	}
	for _, p := range []string{"padding", "color", "min-content", "max-lines", "--ui-card-padding", "border", "gap"} {
		if IsPlacementProperty(p) {
			t.Errorf("%s should not be placement", p)
		}
	}
}

func TestClassNamesDistinct(t *testing.T) {
	sheet, _ := Parse(":scope.fresh { order: 1; }\n.a.fresh { order: 1; }\n.a.size--lg { order: 1; }\n.b .a { order: 1; }\n")
	m, _ := Model(sheet)
	if got, want := m.ClassNames(), []string{"a", "b", "fresh", "size--lg"}; !slices.Equal(got, want) {
		t.Errorf("ClassNames = %v, want %v", got, want)
	}
}
