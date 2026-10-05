package ownstyle

import (
	"strings"
	"testing"
)

// TestKitClassBarUnderEveryPrefix pins GOFASTR1810 to the kit's three
// class prefixes. The cui/hui/fui rename moved the kernel's classes to
// cui-* and headless's to hui-*, and a sheet selecting .cui-hidden or
// .hui-tabs passed with no diagnostic while .fui-* stayed barred.
func TestKitClassBarUnderEveryPrefix(t *testing.T) {
	for _, sel := range []string{".fui-panel", ".cui-hidden", ".card .cui-dropdown", ".hui-tabs__list"} {
		diags := Check("x.style.css", sel+" { display: block; }", KindScoped, defaultTokens(t))
		found := false
		for _, d := range diags {
			if d.Rule == RuleKitClassSelector {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no GOFASTR1810 in %v", sel, diags)
		}
	}
	// An app class that merely starts with the letters is not a kit class.
	diags := Check("x.style.css", ".cuisine { display: block; }", KindScoped, defaultTokens(t))
	for _, d := range diags {
		if d.Rule == RuleKitClassSelector {
			t.Errorf(".cuisine reported as a kit class: %s", d)
		}
	}
}

// TestModelSkipsEveryKitPrefix: kit classes under any of the three
// prefixes never enter the sheet's vocabulary (no CuiHidden() method).
func TestModelSkipsEveryKitPrefix(t *testing.T) {
	sheet, _ := Parse(".card .fui-card__body { gap: 0; }\n.cui-hidden { display: none; }\n.hui-tabs { gap: 0; }\n")
	m, diags := Model(sheet)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	for _, c := range m.Classes {
		if strings.HasPrefix(c.Name, "fui-") || strings.HasPrefix(c.Name, "cui-") || strings.HasPrefix(c.Name, "hui-") {
			t.Errorf("kit class %s entered the model", c.Name)
		}
	}
}

// TestBOMSheetGeneratesAndCompiles: a sheet an editor saved with a UTF-8
// BOM used to pass Check and Model, then fail GenerateFile with
// "illegal byte order mark" blaming the emitter, and Compile scoped a
// selector that matched nothing. The BOM is not CSS; it is dropped
// at the tokenizer the way a CSS decoder drops it.
func TestBOMSheetGeneratesAndCompiles(t *testing.T) {
	src := "\xEF\xBB\xBF.card { color: var(--color-text); }\n"
	if diags := Check("x.style.css", src, KindScoped, defaultTokens(t)); len(diags) != 0 {
		t.Fatalf("check: %v", diags)
	}
	sheet, pd := Parse(src)
	if len(pd) != 0 {
		t.Fatalf("parse: %v", pd)
	}
	m, md := Model(sheet)
	if len(md) != 0 {
		t.Fatalf("model: %v", md)
	}
	if m.class("card") == nil {
		t.Fatalf("card missing from %v", classNames(m))
	}
	out, err := GenerateFile("x", KindScoped, src, m, "x", false)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if strings.Contains(out, "\xEF\xBB\xBF") {
		t.Fatal("the BOM reached the emitted Go")
	}
}
