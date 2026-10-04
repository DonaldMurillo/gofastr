package ownstyle

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// goldenCase pairs a testdata sheet with its compiled form.
type goldenCase struct {
	file string
	name string
	kind Kind
}

var goldenCases = []goldenCase{
	{"review.style.css", "review", KindScoped},
	{"board.style.css", "board", KindScoped},
	{"issuecard.style.css", "issuecard", KindScoped},
	{"app.style.css", "app", KindApp},
	{"nested.style.css", "nested", KindScoped},
}

// compileFile parses and compiles a testdata sheet against the default
// theme's tokens.
func compileFile(t *testing.T, tc goldenCase, tokens map[string]string) string {
	t.Helper()
	src, err := os.ReadFile("testdata/" + tc.file)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	sheet, diags := Parse(string(src))
	if len(diags) != 0 {
		t.Fatalf("%s: parse diagnostics: %v", tc.file, diags)
	}
	out, err := Compile(sheet, tc.name, tc.kind, tokens)
	if err != nil {
		t.Fatalf("%s: compile: %v", tc.file, err)
	}
	return out
}

func TestCompileGoldens(t *testing.T) {
	tokens := style.ThemeToTokens(style.DefaultTheme())
	for _, tc := range goldenCases {
		t.Run(tc.file, func(t *testing.T) {
			got := compileFile(t, tc, tokens)
			want, err := os.ReadFile("testdata/" + tc.file + ".golden")
			if err != nil {
				t.Fatalf("read golden: %v", err)
			}
			if got != strings.TrimRight(string(want), "\n") {
				t.Fatalf("compiled output differs from golden.\n--- golden ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}

func TestCompileCustomMediaFollowTheme(t *testing.T) {
	// A theme that moves md to 800px moves the compiled query: the
	// symbolic (--above-md) is expanded per theme at catalog build.
	tok := style.ThemeToTokens(style.DefaultTheme())
	tok["breakpoint-md"] = "800px"
	got := compileFile(t, goldenCase{"board.style.css", "board", KindScoped}, tok)
	if !strings.Contains(got, "@media (min-width: 800px)") {
		t.Fatalf("expected the md query to follow the theme, got:\n%s", got)
	}
	if strings.Contains(got, "768px") {
		t.Fatalf("stale default breakpoint leaked into output:\n%s", got)
	}
}

func TestCompileBelowCustomMedia(t *testing.T) {
	sheet, diags := Parse("@media (--below-md) { .a { color: red; } }")
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	out, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := `@scope ([data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal])` +
		`{@media (max-width: 767.98px){.a{color:red}}}`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestCompileCombinesCustomMediaWithRawFeatures(t *testing.T) {
	sheet, _ := Parse("@media (--above-md) and (hover: hover) { .a { color: red; } }")
	out, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(out, "@media (min-width: 768px) and (hover: hover){.a{color:red}}") {
		t.Fatalf("combined query wrong:\n%s", out)
	}
}

func TestCompileUnknownCustomMediaIsError(t *testing.T) {
	for _, q := range []string{"--above-3xl", "--below-foo", "--nope"} {
		sheet, _ := Parse("@media (" + q + ") { .a { color: red; } }")
		_, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
		if err == nil || !strings.Contains(err.Error(), "unknown custom media") {
			t.Fatalf("%s: want unknown custom media error, got %v", q, err)
		}
	}
}

func TestCompileInvalidName(t *testing.T) {
	sheet, _ := Parse(".a{color:red}")
	for _, name := range []string{"", "Board", "ui-card", "a/b"} {
		if _, err := Compile(sheet, name, KindScoped, style.ThemeToTokens(style.DefaultTheme())); err == nil {
			t.Errorf("name %q accepted", name)
		}
	}
}

func TestCompileKeyframesRenamedAndReferencesRewritten(t *testing.T) {
	src := ".fresh { animation: settle var(--duration-slow) var(--easing-ease-out) 1; }\n" +
		"@keyframes settle { from { background: red; } }\n" +
		".other { animation-name: settle; }\n"
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	out, err := Compile(sheet, "issuecard", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(out, "animation:issuecard-settle ") {
		t.Fatalf("shorthand reference not rewritten:\n%s", out)
	}
	if !strings.Contains(out, "animation-name:issuecard-settle") {
		t.Fatalf("animation-name reference not rewritten:\n%s", out)
	}
	if !strings.Contains(out, "@keyframes issuecard-settle{from{background:red}}") {
		t.Fatalf("keyframes not renamed/hoisted:\n%s", out)
	}
	// The keyframes block sits OUTSIDE the scope wrap: @keyframes
	// ignore @scope.
	if strings.Index(out, "@keyframes") < strings.LastIndex(out, "@scope") {
		t.Fatalf("keyframes not after the last @scope block:\n%s", out)
	}
}

func TestCompileDarkSplit(t *testing.T) {
	src := "@media (--dark) { .column { background: var(--color-surface); } }"
	sheet, _ := Parse(src)
	out, err := Compile(sheet, "board", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	wantAttr := `@scope ([data-color-scheme="dark"] [data-cui-scope="board"]) to (:scope [data-cui-scope]>*,[data-cui-internal])` +
		`{.column{background:var(--color-surface)}}`
	wantMedia := `@media (prefers-color-scheme: dark){@scope (:root:not([data-color-scheme="light"]) [data-cui-scope="board"]) to (:scope [data-cui-scope]>*,[data-cui-internal])` +
		`{.column{background:var(--color-surface)}}}`
	if !strings.Contains(out, wantAttr) {
		t.Fatalf("attribute variant missing:\n%s", out)
	}
	if !strings.Contains(out, wantMedia) {
		t.Fatalf("prefers-color-scheme variant missing:\n%s", out)
	}
	// No empty main scope block: the sheet had nothing but the dark rule.
	if strings.Contains(out, `to (:scope [data-cui-scope]>*,[data-cui-internal]){}`) {
		t.Fatalf("empty main block emitted:\n%s", out)
	}
}

func TestCompileDarkSplitAppRoots(t *testing.T) {
	sheet, _ := Parse("@media (--dark) { .a { color: red; } }")
	out, err := Compile(sheet, "app", KindApp, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(out, `@scope (:root[data-color-scheme="dark"]) to ([data-cui-internal]){.a{color:red}}`) {
		t.Fatalf("app attribute variant missing:\n%s", out)
	}
	if !strings.Contains(out, `@scope (:root:not([data-color-scheme="light"])) to ([data-cui-internal]){.a{color:red}}`) {
		t.Fatalf("app media variant missing:\n%s", out)
	}
}

func TestCompileAppWrap(t *testing.T) {
	sheet, _ := Parse(".figure { letter-spacing: -0.01em; }")
	out, err := Compile(sheet, "app", KindApp, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := `@scope (:root) to ([data-cui-internal]){.figure{letter-spacing:-0.01em}}`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

func TestCompileMinifySeparators(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		// calc's signed operands keep their spaces.
		{".a{width:calc(100% - 2px)}", `.a{width:calc(100% - 2px)}`},
		// + and - between a var() and its operand keep both spaces:
		// calc(var(--x)+ 1px) is invalid and drops the declaration.
		{".a{width:calc(var(--size-page-width) + 2 * var(--size-page-gutter))}", `.a{width:calc(var(--size-page-width) + 2*var(--size-page-gutter))}`},
		{".a{inset:calc(var(--size-header-height) - 1px) 0 0}", `.a{inset:calc(var(--size-header-height) - 1px) 0 0}`},
		{".a{margin:calc(-1 * var(--spacing-sm)) calc(2px + (var(--spacing-sm)))}", `.a{margin:calc(-1*var(--spacing-sm)) calc(2px + (var(--spacing-sm)))}`},
		// Descendant combinator survives; compound does not split.
		{".column.over-limit .count{color:red}", `.column.over-limit .count{color:red}`},
		// Strings juxtaposed keep one space; commas do not.
		{".a{grid-template-areas:\"x\" \"y\"}", `.a{grid-template-areas:"x" "y"}`},
		{".a{transition:box-shadow var(--duration-fast) var(--easing-ease-out)}", `.a{transition:box-shadow var(--duration-fast) var(--easing-ease-out)}`},
		// A close paren before a value word keeps its space.
		{".a{background:color-mix(in srgb, var(--color-primary) 14%, var(--color-surface))}", `.a{background:color-mix(in srgb,var(--color-primary) 14%,var(--color-surface))}`},
		// The font shorthand's slash needs no spaces.
		{"h2{font:600 var(--text-sm)/1.3 var(--font-heading)}", `h2{font:600 var(--text-sm)/1.3 var(--font-heading)}`},
		// Comments vanish.
		{".a{ /* c */ color:red /* d */ ; }", ".a{color:red}"},
		// Trailing !important loses its inner space, keeps the flag.
		{".a{color:red !important}", ".a{color:red!important}"},
		// Custom property values are verbatim.
		{".a{--x:  1   2 }", ".a{--x:1   2}"},
	}
	for _, tc := range cases {
		sheet, diags := Parse(tc.src)
		if len(diags) != 0 {
			t.Errorf("%q: parse diags %v", tc.src, diags)
			continue
		}
		out, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
		if err != nil {
			t.Errorf("%q: %v", tc.src, err)
			continue
		}
		want := `@scope ([data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal]){` + tc.want + `}`
		if out != want {
			t.Errorf("%q:\n got %s\nwant %s", tc.src, out, want)
		}
	}
}

func TestCompileDoesNotMutateSheet(t *testing.T) {
	src := ".a { animation: spin 1s; } @keyframes spin { to { opacity: 1; } }"
	sheet, _ := Parse(src)
	tok := style.ThemeToTokens(style.DefaultTheme())
	if _, err := Compile(sheet, "x", KindScoped, tok); err != nil {
		t.Fatal(err)
	}
	if _, err := Compile(sheet, "x", KindScoped, tok); err != nil {
		t.Fatal(err)
	}
	// The second compile must have seen the ORIGINAL name (no double
	// rename: x-spin-spin).
	out, _ := Compile(sheet, "x", KindScoped, tok)
	if strings.Contains(out, "x-x-") || strings.Contains(out, "spin-spin") {
		t.Fatalf("compile mutated the parsed sheet: %s", out)
	}
	if !strings.Contains(out, "animation:x-spin") {
		t.Fatalf("reference not rewritten: %s", out)
	}
}

func TestCompileEmptySheet(t *testing.T) {
	sheet, _ := Parse("/* only a comment */")
	out, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if out != "" {
		t.Fatalf("empty sheet compiled to %q", out)
	}
}

func TestCompileNestedRulesKeepDeclTerminators(t *testing.T) {
	src := ".a { color: var(--color-text); &:hover { color: var(--color-primary); } .b & { color: var(--color-text); } }"
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	out, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatal(err)
	}
	// The `;` before the first nested rule is load-bearing.
	want := `@scope ([data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal])` +
		`{.a{color:var(--color-text);&:hover{color:var(--color-primary)}.b &{color:var(--color-text)}}}`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	// Declarations render before nested rules (the CSS nesting
	// convention; the parser stores them apart), and the LAST
	// declaration before nested rules keeps its ';' while a trailing
	// declaration before the block's '}' does not.
	src = ".a { color: red; .b { color: blue; } padding: 0 }"
	sheet, _ = Parse(src)
	out, _ = Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if !strings.Contains(out, "{.a{color:red;padding:0;.b{color:blue}}}") {
		t.Fatalf("terminator placement wrong:\n%s", out)
	}
}

func TestCompileDarkNestedInsideMediaLeavesNoJunk(t *testing.T) {
	src := "@media (--above-md) { @media (--dark) { .a { color: red; } } }"
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	out, err := Compile(sheet, "x", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatal(err)
	}
	want := `@media (min-width: 768px){@scope ([data-color-scheme="dark"] [data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal]){.a{color:red}}}` +
		`@media (prefers-color-scheme: dark) and (min-width: 768px){@scope (:root:not([data-color-scheme="light"]) [data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal]){.a{color:red}}}`
	if out != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
}

// reMediaCustom matches a custom-medium name left inside a media
// prelude after compilation.
var reMediaCustom = regexp.MustCompile(`@media[^{]*--`)

func TestCompileNoCustomMediaSurvives(t *testing.T) {
	// Every custom medium must be expanded away in every output: a
	// surviving symbolic name in a media prelude is a query the
	// browser cannot parse. (Scanning bare "(--" would false-positive
	// on every var(--token) reference.)
	tokens := style.ThemeToTokens(style.DefaultTheme())
	sources := []string{
		"@media (--above-md) { @media (--dark) { .a { color: red; } } }",
		"@media (--dark) { .a { color: red; } }",
		"@media (--reduced-motion) and (--above-lg) { .a { animation: none; } }",
		"@container (min-width: 26rem) { .a { color: red; } }",
	}
	for _, src := range sources {
		sheet, _ := Parse(src)
		out, err := Compile(sheet, "x", KindScoped, tokens)
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if reMediaCustom.MatchString(out) {
			t.Errorf("%q: custom media survived compilation:\n%s", src, out)
		}
	}
	// And none of the goldens carry one either.
	for _, tc := range goldenCases {
		got := compileFile(t, tc, tokens)
		if reMediaCustom.MatchString(got) {
			t.Errorf("%s: custom media survived compilation:\n%s", tc.file, got)
		}
	}
}

func TestCompileDarkNestedInStyleRule(t *testing.T) {
	tokens := style.ThemeToTokens(style.DefaultTheme())
	attr := `@scope ([data-color-scheme="dark"] [data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal])`
	mediaInner := `@scope (:root:not([data-color-scheme="light"]) [data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal])`

	// Directly inside a style rule: the declarations apply to .a under
	// both dark roots, and nothing is silently dropped.
	src := ".a { @media (--dark) { color: var(--color-text); } }"
	out, err := Compile(mustParse(t, src), "x", KindScoped, tokens)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	want := attr + "{.a{color:var(--color-text)}}" +
		"@media (prefers-color-scheme: dark){" + mediaInner + "{.a{color:var(--color-text)}}}"
	if out != want {
		t.Fatalf("direct:\ngot  %s\nwant %s", out, want)
	}

	// One level deeper: the context selector composes (.a:hover).
	src = ".a { &:hover { @media (--dark) { color: var(--color-primary); } } }"
	out, err = Compile(mustParse(t, src), "x", KindScoped, tokens)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// The chain emits the ancestor rules as written; the browser's
	// nesting resolves &:hover to .a:hover.
	want = attr + "{.a{&:hover{color:var(--color-primary)}}}" +
		"@media (prefers-color-scheme: dark){" + mediaInner + "{.a{&:hover{color:var(--color-primary)}}}}"
	if out != want {
		t.Fatalf("deeper:\ngot  %s\nwant %s", out, want)
	}

	// Beside ordinary declarations: the light one stays in the main
	// scope, the dark one moves to the splits.
	src = ".a { color: var(--color-text); @media (--dark) { color: var(--color-primary); } }"
	out, err = Compile(mustParse(t, src), "x", KindScoped, tokens)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(out, `{.a{color:var(--color-text)}}`) {
		t.Fatalf("light declaration lost:\n%s", out)
	}
	if !strings.Contains(out, attr+"{.a{color:var(--color-primary)}}") {
		t.Fatalf("dark declaration missing:\n%s", out)
	}

	// A nested style rule inside the dark block stays nested under its
	// parent as written (the browser resolves .a .b).
	src = ".a { @media (--dark) { .b { color: var(--color-text); } } }"
	out, err = Compile(mustParse(t, src), "x", KindScoped, tokens)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if !strings.Contains(out, attr+"{.a{.b{color:var(--color-text)}}}") {
		t.Fatalf("nested rule lost its parent:\n%s", out)
	}
}

func TestCompileTopLevelDarkWithDeclarationsIsError(t *testing.T) {
	// A (--dark) block at the top level holding bare declarations has
	// no selector to apply to: an error, never a silent drop.
	_, err := Compile(mustParse(t, "@media (--dark) { color: red; }"), "x", KindScoped,
		style.ThemeToTokens(style.DefaultTheme()))
	if err == nil || !strings.Contains(err.Error(), "(--dark)") {
		t.Fatalf("want an error naming (--dark), got %v", err)
	}
}

func TestCompileNonEmptyRulesNeverVanish(t *testing.T) {
	// A non-empty rule set must compile to output or report an error —
	// never silently to the empty string.
	tokens := style.ThemeToTokens(style.DefaultTheme())
	inputs := []string{
		".a { @media (--dark) { color: var(--color-text); } }",
		".a { &:hover { @media (--dark) { color: var(--color-primary); } } }",
		".a { color: var(--color-text); @media (--dark) { color: var(--color-primary); } }",
		"@media (--dark) { .column { background: var(--color-surface); } }",
		"@media (--above-md) { @media (--dark) { .a { color: red; } } }",
		".a { animation: spin 1s; } @keyframes spin { to { opacity: 1; } }",
		".a { color: red; }",
	}
	for _, src := range inputs {
		out, err := Compile(mustParse(t, src), "x", KindScoped, tokens)
		if err != nil {
			continue // errors are the honest answer for unsupported shapes
		}
		if out == "" {
			t.Errorf("%q compiled to the empty string", src)
		}
	}
	// And every golden is non-empty for its non-empty sheet.
	for _, tc := range goldenCases {
		if got := compileFile(t, tc, tokens); got == "" {
			t.Errorf("%s compiled to the empty string", tc.file)
		}
	}
}

func TestCompileDarkChainKeepsSelectorLists(t *testing.T) {
	tokens := style.ThemeToTokens(style.DefaultTheme())
	attr := `@scope ([data-color-scheme="dark"] [data-cui-scope="x"]) to (:scope [data-cui-scope]>*,[data-cui-internal])`
	compile := func(t *testing.T, src string) string {
		t.Helper()
		out, err := Compile(mustParse(t, src), "x", KindScoped, tokens)
		if err != nil {
			t.Fatalf("%q: compile: %v", src, err)
		}
		return out
	}
	// Each hoisted dark split wraps the ancestor chain AS WRITTEN: the
	// browser's nesting resolves lists, & and implicit descendants
	// (& means :is(<parent list>)).
	rows := []struct {
		src  string
		want string // the attribute-variant block's body
	}{
		{".a, .b { .c { @media (--dark) { color: var(--color-text); } } }", attr + `{.a,.b{.c{color:var(--color-text)}}}`},
		{".a, .b { .x & { @media (--dark) { color: var(--color-text); } } }", attr + `{.a,.b{.x &{color:var(--color-text)}}}`},
		{".a { .c, .d { @media (--dark) { color: var(--color-text); } } }", attr + `{.a{.c,.d{color:var(--color-text)}}}`},
		{".a { &.on, &:hover { @media (--dark) { color: var(--color-text); } } }", attr + `{.a{&.on,&:hover{color:var(--color-text)}}}`},
		// A non-dark wrapper stays in the chain at its position.
		{".a { @media (--above-md) { @media (--dark) { color: var(--color-text); } } }", attr + `{.a{@media (min-width: 768px){color:var(--color-text)}}}`},
		// Dark inside dark: the inner wrapper drops (already dark).
		{".a { @media (--dark) { .b { @media (--dark) { color: var(--color-text); } } } }", attr + `{.a{.b{color:var(--color-text)}}}`},
	}
	for _, tc := range rows {
		out := compile(t, tc.src)
		if !strings.Contains(out, tc.want) {
			t.Errorf("%q:\nwant body %s\ngot      %s", tc.src, tc.want, out)
		}
		// Every output of these shapes stays free of symbolic media.
		if reMediaCustom.MatchString(out) {
			t.Errorf("%q: custom media survived:\n%s", tc.src, out)
		}
	}
}
