package analyzers_test

import (
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"testing"
)

// A stylesheet FILE with no owner is GOFASTR1809. It used to be
// GOFASTR1801's stylesheet arm; the skips are the ones that arm had
// (design-system trees, testdata, sheets that only assign custom
// properties) and so are the allow forms, spelled with the new id.

func TestOwnerlessStylesheet(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":                 "package main\nimport _ \"embed\"\n//go:embed app.css\nvar css string\n",
		"app.css":                 "/* embedded stylesheet */\n.card { display: flex; }\n.other { margin: 0; }\n",
		"static/site.css":         "@import url('other.css');\n",
		"core-ui/style/owned.css": ".card { display: flex; }\n",
		"empty.css":               "/* documentation only */\n",
	})
	found := countRule(t, ds, contracts.RuleOwnerlessStylesheet)
	if len(found) != 2 {
		t.Fatalf("want 2 stylesheet findings, got %v", found)
	}
	for _, d := range found {
		if d.File != "app.css" && d.File != "static/site.css" {
			t.Errorf("unexpected stylesheet: %s", d.File)
		}
		if d.File == "app.css" && d.Line != 2 {
			t.Errorf("embedded stylesheet finding line = %d, want 2", d.Line)
		}
	}
}

// One file, one finding: the stylesheet that 1809 reports is not also
// a GOFASTR1801 finding, and an owned style (*.style.css) is neither.
func TestStylesheetNeverBespokeCSS(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css":               ".card { display: flex; }\n",
		"board/board.style.css": ".column { display: grid; }\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "a stylesheet file is GOFASTR1809's finding, never 1801's")
	for _, d := range countRule(t, ds, contracts.RuleOwnerlessStylesheet) {
		if d.File != "app.css" {
			t.Errorf("an owned style was reported as ownerless: %s", d.File)
		}
	}
	assertHas(t, ds, contracts.RuleOwnerlessStylesheet)
}

func TestOwnerlessStylesheetAllow(t *testing.T) {
	for _, directive := range []string{"allow(GOFASTR1809)", "allow(rendering/ownerless-stylesheet)", "allow-file(GOFASTR1809)"} {
		t.Run(directive, func(t *testing.T) {
			ds := fixture(t, map[string]string{"app.css": "/* gofastr:" + directive + " vendor stylesheet kept unchanged */\n.card { display: flex; }\n.other { margin: 0; }\n"})
			assertNot(t, ds, contracts.RuleOwnerlessStylesheet, "leading CSS comment explicitly permits this sheet")
			assertNot(t, ds, contracts.RuleSuppressionStale, "the stylesheet finding consumes the directive")
		})
	}
	ds := fixture(t, map[string]string{"app.css": "/* gofastr:allow(GOFASTR1809) */\n.card { display: flex; }\n"})
	assertHas(t, ds, contracts.RuleOwnerlessStylesheet)
}

func TestOwnerlessStylesheetTokensAndTestdata(t *testing.T) {
	ds := fixture(t, map[string]string{
		"tokens.css":                     ":root { --brand: #fff; --font: \"display: flex;\"; }\n.layout:hover { --width: clamp(1rem, 2vw, 4rem) }\n",
		"nested.css":                     "@media (width > 30rem) { :root { --brand: red; } }\n",
		"mixed.css":                      ":root { --brand: red; }\n.card { display: flex; }\n",
		"testdata/style.css":             ".fixture { display: flex; }\n",
		"core/static/testdata/style.css": ".fixture { display: flex; }\n",
		"testdata-like/style.css":        ".real { display: flex; }\n",
	})
	found := countRule(t, ds, contracts.RuleOwnerlessStylesheet)
	if len(found) != 2 {
		t.Fatalf("want only mixed and real stylesheets, got %v", found)
	}
	for _, d := range found {
		if d.File != "mixed.css" && d.File != "testdata-like/style.css" {
			t.Errorf("token-only or testdata stylesheet reported: %s", d.File)
		}
	}
}
