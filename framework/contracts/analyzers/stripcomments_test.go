package analyzers_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// The text rules scan a copy of each file with its comments removed. The
// stripper used to open a block comment at any "/*", string literals
// included, so a glob or an Accept header blanked every later line from
// GOFASTR1401/1801-1804 and made GOFASTR1402 miss a CSRF call.

func TestGlobStringKeepsLaterCSSVisible(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nvar pattern = \"locales/*.json\"\n\nconst css = `.card { border-radius: 8px; }`\n",
	})
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestAcceptHeaderKeepsLaterNavVisible(t *testing.T) {
	ds := fixture(t, map[string]string{
		"js.go": "package main\n\nvar accept = `*/*`\nvar glob = '/'\nvar any = \"*/*\"\n\nconst a = \"image/*\"\n\nconst script = `location.href = '/orders'`\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestURLStringKeepsRestOfLine(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nvar u, css = \"https://cdn.example\", `.card { border-radius: 8px; }`\n",
	})
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestFileInputFormCountsItsCSRF(t *testing.T) {
	ds := fixture(t, map[string]string{
		"form.go": "package main\n\nvar CSRFInputFromCtx = func(any) string { return \"\" }\n\n" +
			"func page(ctx any) string {\n" +
			"\treturn `<form method=\"POST\" enctype=\"multipart/form-data\"><input type=\"file\" accept=\"image/*\">` +\n" +
			"\t\tCSRFInputFromCtx(ctx) + `</form>`\n}\n",
	})
	assertNot(t, ds, contracts.RuleFormWithoutCSRF, "the form wires CSRFInputFromCtx after an image/* accept")
}

func TestBacktickRuneDoesNotHideComments(t *testing.T) {
	// A rune holding a backtick is not a raw string: the comment after it
	// is still prose and must stay out of the scan.
	ds := fixture(t, map[string]string{
		"doc.go": "package main\n\nvar tick = '`'\n\n// fontFaceCSS holds the @font-face rules for the theme's fonts.\nvar fontFaceCSS string\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "prose about CSS after a backtick rune is not CSS")
}

func TestBlockCommentStillStripped(t *testing.T) {
	ds := fixture(t, map[string]string{
		"doc.go": "package main\n\n/*\n.card { border-radius: 8px; }\n*/\nvar x = 1 /* @font-face */\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "CSS inside a block comment is prose")
}

func TestEmittedCommentStringIsProse(t *testing.T) {
	// A generator writes Go comments into a string; their text is prose
	// about the emitted code, the same as a comment here would be.
	ds := fixture(t, map[string]string{
		"gen.go": "package main\n\nvar src = \"// fontFaceCSS holds the @font-face rules\\n\" +\n" +
			"\t\"// for every surface.\\n\"\n\nconst css = `.card { color: red; } /* knob notes:\n@media is not used here */`\n",
	})
	for _, d := range ds {
		if d.RuleID == contracts.RuleBespokeCSS && d.Line != 6 {
			t.Errorf("1801 at line %d: prose in an emitted comment counted as CSS", d.Line)
		}
	}
}
