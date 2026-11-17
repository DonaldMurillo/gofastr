package analyzers_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/framework/contracts/analyzers"
)

func TestSweepStylingMaxWidthInMediaIsNotBespokeCSS(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nfunc f(ss sheet) { ss.Media(\"(max-width: 900px)\", func(r rule) { r.Set() }) }\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "a hyphenated property's tail is not its shorter cousin")
}

func TestSweepStylingCapitalisedFieldIsNotCSS(t *testing.T) {
	ds := fixture(t, map[string]string{
		"theme.go": "package main\n\nvar t = Theme{Colors: Palette{\n\tBackground: \"#15141B\",\n\tColor: \"#FFFFFF\",\n}}\n",
	})
	assertNot(t, ds, contracts.RuleBespokeCSS, "a capitalised Go field holding a hex colour is a theme token")
}

func TestSweepStylingHeadlessScriptOwnsNavigation(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":                      "package main\n",
		"framework/headless/js/nav.js": "if (!window.gofastrNav) { location.assign(href); }\n",
	})
	assertNot(t, ds, contracts.RuleHardNavigation, "the headless behaviours fall back to a load by design")
}

func TestSweepStylingBareLocationAssignInScript(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":       "package main\n",
		"static/app.js": "function go(next) {\n  location = next;\n}\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestSweepStylingDeepQualifiedHrefIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"main.go":       "package main\n",
		"static/app.js": "window.top.location.href = next;\n",
	})
	assertHas(t, ds, contracts.RuleHardNavigation)
}

func TestSweepStylingLongTriggerIsClippedOnRunes(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nconst css = `.a { font-family: a" + strings.Repeat("é", 40) + "; }`\n",
	})
	d := assertHas(t, ds, contracts.RuleBespokeCSS)
	if !utf8.ValidString(d.Message) || !strings.Contains(d.Message, "…") {
		t.Fatalf("clipped trigger must be valid UTF-8 and marked: %q", d.Message)
	}
}

func TestSweepStylingStyleTagWithoutColonIsReported(t *testing.T) {
	ds := fixture(t, map[string]string{
		"page.go": "package main\n\nfunc f(w writer) { w.Write([]byte(\"<style>\")) }\n",
	})
	assertHas(t, ds, contracts.RuleBespokeCSS)
}

func TestSweepStylingSupportsConditionDeclaresNothing(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": "@media screen { @supports (--brand: red) { .a { color: var(--brand); } } }\n",
	})
	if found := countRule(t, ds, contracts.RuleUnknownThemeToken); len(found) != 1 {
		t.Fatalf("a feature query's condition is not a declaration, got %v", found)
	}
}

func TestSweepStylingQuoteInCSSCommentIsProse(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": "/* don't */ .x { color: var(--radius-lg); }\n",
	})
	assertHas(t, ds, contracts.RuleUnknownThemeToken)
}

func TestSweepStylingCustomPropertyIsNotATokenBypass(t *testing.T) {
	ds := designFixture(t, "core-ui/style/page.go",
		"package style\n\nvar css = `.eyebrow { --font-size: 0.75rem; }`\n")
	if found := countRule(t, ds, contracts.RuleHardcodedTokenValue); len(found) != 0 {
		t.Fatalf("a custom property named like a property is not that property, got %v", found)
	}
}

func TestSweepStylingKeywordAndFontFallbacksAreQuiet(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { color: var(--color-text, inherit); font-family: var(--font-mono, Arial); }`\n")
	if found := countRule(t, ds, contracts.RuleFallbackDrift); len(found) != 0 {
		t.Fatalf("inherit and a font stack are degraded-mode choices, got %v", found)
	}
}

func TestSweepStylingURLSlashesAreNotACSSComment(t *testing.T) {
	ds := fixture(t, map[string]string{
		"app.css": ".a { background: url(https://cdn.example/x.png); color: var(--radius-lg); }\n",
	})
	assertHas(t, ds, contracts.RuleUnknownThemeToken)
}

func sweepTwoCards(t *testing.T, files map[string]string) []contracts.Diagnostic {
	t.Helper()
	files["one/one.go"] = "package one\n\nvar One = 1\n"
	files["two/two.go"] = "package two\n\nvar Two = 2\n"
	return fixture(t, merge(files,
		ownedPair(t, "one", "one", "card", ".a { order: 1; }\n"),
		ownedPair(t, "two", "two", "card", ".b { order: 1; }\n"),
	))
}

func TestSweepStylingUnixTagSkipsWindows(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go":           "package main\n\nfunc main() {}\n",
		"cmd/app/styles_windows.go": "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n",
		"cmd/app/alt.go":            "//go:build unix\n\npackage main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "windows is not unix")
}

func TestSweepStylingCustomTagCountsAsSet(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go": "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n\nfunc main() {}\n",
		"cmd/app/alt.go":  "//go:build extra\n\npackage main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertHas(t, ds, contracts.RuleDuplicateStyleName)
}

func TestSweepStylingUnderscoreFileNeverBuilds(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go": "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n\nfunc main() {}\n",
		"cmd/app/_alt.go": "package main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "the go tool ignores a file named _alt.go")
}

const sweepMainOne = "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n\nfunc main() {}\n"

func TestSweepStylingMalformedConstraintNeverBuilds(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go": sweepMainOne,
		"cmd/app/alt.go":  "//go:build linux &&\n\npackage main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "the compiler refuses a malformed //go:build")
}

func TestSweepStylingConstraintBelowPackageIsAComment(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go": sweepMainOne,
		"cmd/app/alt.go":  "package main\n\n//go:build ignore\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertHas(t, ds, contracts.RuleDuplicateStyleName)
}

func TestSweepStylingLegacyPlusBuildIsRead(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go": sweepMainOne,
		"cmd/app/alt.go":  "// +build ignore\n\npackage main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "+build ignore keeps the file out")
}

func TestSweepStylingOSArchSuffixPairConstrains(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go":             "package main\n\nfunc main() {}\n",
		"cmd/app/styles_linux.go":     "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n",
		"cmd/app/alt_darwin_arm64.go": "package main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "no platform is both linux and darwin")
}

func TestSweepStylingArchSuffixConstrains(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go":         "package main\n\nfunc main() {}\n",
		"cmd/app/styles_amd64.go": "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n",
		"cmd/app/alt_arm64.go":    "package main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "no platform is both amd64 and arm64")
}

func TestSweepStylingReverseKeywordOnAnotherPropertyIsQuiet(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { --dir: row-reverse; animation-direction: row-reverse; }`\n")
	assertNot(t, ds, contracts.RuleFlexReverse, "only flex-direction and flex-flow order children")
}

func TestSweepStylingSharedSheetFindingIsReportedOnce(t *testing.T) {
	const css = ".a { color: var(--nope-x); }\n"
	dir := t.TempDir()
	seedFixture(t, dir, map[string]string{
		"go.mod":        "module example.com/app\n\ngo 1.26\n",
		"cmd/a/main.go": "package main\n\nimport _ \"example.com/app/lib\"\n\nfunc main() {}\n",
		"cmd/b/main.go": "package main\n\nimport _ \"example.com/app/lib\"\n\nfunc main() {}\n",
		"lib/lib.go":    "package lib\n",
	})
	pass, err := contracts.NewPass(dir, contracts.DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	got := analyzers.CheckStylePrograms(pass, []analyzers.SheetInput{{Path: "lib/card.style.css", Src: css}}, nil)
	if fs := got.SheetFindings["lib/card.style.css"]; len(fs) != 1 {
		t.Fatalf("one sheet, one problem, ten groups: want 1 finding, got %v", fs)
	}
}

func TestSweepStylingEveryShippedPlatformIsGrouped(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/a/main_darwin.go": "package main\n\nimport \"example.com/app/one\"\n\nvar _ = one.One\n\nfunc main() {}\n",
		"cmd/b/main_darwin.go": "package main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n\nfunc main() {}\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "each darwin binary links one card")
}

func TestSweepStylingTestFilesAreNotLinked(t *testing.T) {
	ds := sweepTwoCards(t, map[string]string{
		"cmd/app/main.go":      sweepMainOne,
		"cmd/app/main_test.go": "package main\n\nimport \"example.com/app/two\"\n\nvar _ = two.Two\n",
	})
	assertNot(t, ds, contracts.RuleDuplicateStyleName, "a test file is not in the binary")
}

func TestSweepStylingImportantIsNotPartOfTheEasing(t *testing.T) {
	ds := designFixture(t, "framework/ui/card.go",
		"package ui\n\nvar css = `.card { transition-timing-function: ease!important; }`\n")
	assertHas(t, ds, contracts.RuleBareThemeLiteral)
}
