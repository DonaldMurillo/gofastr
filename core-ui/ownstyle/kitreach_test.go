package ownstyle

import "testing"

// GOFASTR1810 compared a class selector's raw text and read only an
// attribute selector's name, so an escaped kit class and a [class…]
// match on a kit prefix both styled kit markup with no diagnostic.

func kitSelectorHits(t *testing.T, sel string) int {
	t.Helper()
	n := 0
	for _, d := range Check("x.style.css", sel+" { display: block; }", KindScoped, defaultTokens(t)) {
		if d.Rule == RuleKitClassSelector {
			n++
		}
	}
	return n
}

func TestKitClassReachedByAttrOrEscape(t *testing.T) {
	for _, sel := range []string{
		`.col [class~="fui-card-header"]`,
		`.col [class~=fui-card-header]`,
		`.col [class^="fui-"]`,
		`.col [class^="fui"]`,
		`.col [class|="hui"]`,
		`.col [class*="cui-"]`,
		`.col [class*="ui-"]`,
		`.col [class="x fui-card"]`,
		`.col [CLASS~="fui-card"]`,
		`.col [cl\61ss~="fui-card"]`,
		`.col [class~="FUI-card" i]`,
		`.col [class~="\66 ui-card"]`,
		`.col .\66 ui-card-header`,
		`.col .\66ui-card-header`,
		`.col .f\ui-card`,
		`.col [DATA-CUI-scope]`,
		`.col [data-\63 ui-scope]`,
	} {
		if kitSelectorHits(t, sel) != 1 {
			t.Errorf("%s: want one GOFASTR1810", sel)
		}
	}
}

func TestAppClassAttrAndEscapePass(t *testing.T) {
	for _, sel := range []string{
		`.col [class~="my-card"]`,
		`.col [class^="card"]`,
		`.col [class*="card"]`,
		`.col [class*="-"]`,
		`.col [class~="my-fui-thing"]`,
		`.col [class~="FUI-card"]`,
		`.col [class]`,
		`.col [title~="fui-card"]`,
		`.my-fui-thing`,
		`.col .\63 ard`,
		`.col .a\:b`,
	} {
		if n := kitSelectorHits(t, sel); n != 0 {
			t.Errorf("%s: %d GOFASTR1810, want none", sel, n)
		}
	}
}
