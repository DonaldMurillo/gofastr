package ownstyle

import (
	"strings"
	"testing"
)

func parseOne(t *testing.T, src string) (*Rule, []Diagnostic) {
	t.Helper()
	sheet, diags := Parse(src)
	if len(sheet.Rules) == 0 {
		t.Fatalf("parse %q: no rules (diags %v)", src, diags)
	}
	return sheet.Rules[0], diags
}

func TestParseStyleRuleDecls(t *testing.T) {
	r, diags := parseOne(t, ".card{padding:16px;color:#52525B;outline:2px solid var(--color-primary) !important}")
	if len(diags) != 0 {
		t.Fatalf("unexpected diags: %v", diags)
	}
	if r.Text != ".card" || r.Block != true || r.At {
		t.Fatalf("rule shape: %+v", r)
	}
	if len(r.Decls) != 3 {
		t.Fatalf("want 3 decls, got %d", len(r.Decls))
	}
	d := r.Decls[0]
	if d.Name != "padding" || d.ValueSrc != "16px" || d.Pos != (Pos{1, 7}) || d.ValuePos != (Pos{1, 15}) {
		t.Errorf("decl 0: %+v", d)
	}
	if d.Important {
		t.Errorf("decl 0 flagged important")
	}
	imp := r.Decls[2]
	if !imp.Important || imp.Name != "outline" {
		t.Fatalf("decl 2: %+v", imp)
	}
	// The '!' sits at column 73.
	if imp.ImpPos != (Pos{1, 73}) {
	}
	// Value tokens exclude trivia and the !important pair.
	if got := tokenRunText(imp.Value); got != "2px solid var( --color-primary )" && !strings.Contains(got, "var(") {
		t.Errorf("value tokens: %q", got)
	}
}

func TestParseNestedAtRules(t *testing.T) {
	src := "@media (--above-md) {\n  .columns { grid-auto-columns: minmax(16rem, 1fr); }\n  @media (--reduced-motion) {\n    .x { animation: none; }\n  }\n}\n"
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if len(sheet.Rules) != 1 {
		t.Fatalf("rules: %d", len(sheet.Rules))
	}
	m := sheet.Rules[0]
	if !m.At || m.Name != "media" || m.Text != "@media (--above-md)" {
		t.Fatalf("media rule: %+v", m)
	}
	if len(m.Nested) != 2 {
		t.Fatalf("nested: %d", len(m.Nested))
	}
	if m.Nested[0].Text != ".columns" || len(m.Nested[0].Decls) != 1 {
		t.Errorf("inner rule: %+v", m.Nested[0])
	}
	inner := m.Nested[1]
	if !inner.At || inner.Name != "media" || len(inner.Nested) != 1 {
		t.Fatalf("inner media: %+v", inner)
	}
	if inner.Nested[0].Text != ".x" {
		t.Errorf("innermost: %+v", inner.Nested[0])
	}
}

func TestParseNestedStyleRules(t *testing.T) {
	src := ".card { padding: 8px; & .title { font-weight: 700; } .meta { color: red; } }"
	r, diags := parseOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if len(r.Decls) != 1 || len(r.Nested) != 2 {
		t.Fatalf("decls %d nested %d", len(r.Decls), len(r.Nested))
	}
	if r.Nested[0].Text != "& .title" || r.Nested[1].Text != ".meta" {
		t.Errorf("nested texts: %q %q", r.Nested[0].Text, r.Nested[1].Text)
	}
}

func TestParseKeyframes(t *testing.T) {
	src := "@keyframes settle {\n  from { background: red; }\n  50% { opacity: .4; }\n  to { background: blue; }\n}\n"
	r, diags := parseOne(t, src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if !r.At || r.Name != "keyframes" || len(r.Nested) != 3 {
		t.Fatalf("keyframes rule: %+v nested %d", r, len(r.Nested))
	}
	if r.Nested[1].Text != "50%" || len(r.Nested[1].Decls) != 1 {
		t.Errorf("50%% step: %+v", r.Nested[1])
	}
}

func TestParseDocComments(t *testing.T) {
	src := `/* The board screen. */
:scope { display: grid; }
/* A flag: more cards than
   the column's WIP limit. */
/* Second paragraph attaches too. */
.column.over-limit .count { color: red; }
`
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if len(sheet.Rules) != 2 {
		t.Fatalf("rules: %d", len(sheet.Rules))
	}
	if sheet.Rules[0].Doc != "The board screen." {
		t.Errorf("rule 0 doc: %q", sheet.Rules[0].Doc)
	}
	want := "A flag: more cards than\nthe column's WIP limit.\nSecond paragraph attaches too."
	if sheet.Rules[1].Doc != want {
		t.Errorf("rule 1 doc: %q", sheet.Rules[1].Doc)
	}
}

func TestParseTrailingCommentIsNotDoc(t *testing.T) {
	src := ".a { color: red; }   /* trailing remark */\n.b { color: blue; }\n"
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if sheet.Rules[1].Doc != "" {
		t.Errorf("trailing comment leaked into doc: %q", sheet.Rules[1].Doc)
	}
	// But a comment on its own line still attaches.
	src = ".a { color: red; }\n/* own line */\n.b { color: blue; }\n"
	sheet, _ = Parse(src)
	if sheet.Rules[1].Doc != "own line" {
		t.Errorf("own-line comment: %q", sheet.Rules[1].Doc)
	}
}

func TestParseStatementAtRule(t *testing.T) {
	src := "@import url(theme.css);\n.a { color: red; }\n"
	sheet, diags := Parse(src)
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if !sheet.Rules[0].At || sheet.Rules[0].Name != "import" || sheet.Rules[0].Block {
		t.Fatalf("import rule: %+v", sheet.Rules[0])
	}
	if sheet.Rules[0].Text != "@import url(theme.css)" {
		t.Errorf("import text: %q", sheet.Rules[0].Text)
	}
}

func TestParseCustomPropertyDeclaration(t *testing.T) {
	r, diags := parseOne(t, ".a{--accent:#B91C1C;--trail: var(--accent, 4px)}")
	if len(diags) != 0 {
		t.Fatalf("diags: %v", diags)
	}
	if r.Decls[0].Name != "--accent" || r.Decls[0].ValueSrc != "#B91C1C" {
		t.Errorf("custom prop: %+v", r.Decls[0])
	}
	if r.Decls[1].ValueSrc != "var(--accent, 4px)" {
		t.Errorf("custom prop value: %q", r.Decls[1].ValueSrc)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		src  string
		want string
	}{
		{".a { color: red", "unclosed '{'"},
		{".a { color red; }", "has no ':'"},
		{".a { 12px: red; }", "expected a property name"},
		{".a { color: \"unterminated", "unterminated string"},
		{".a { background: url(a b); }", "invalid url()"},
		{".a { color: red; }", ""}, // clean
	}
	for _, tc := range cases {
		_, diags := Parse(tc.src)
		if tc.want == "" {
			if len(diags) != 0 {
				t.Errorf("%q: unexpected diags %v", tc.src, diags)
			}
			continue
		}
		found := false
		for _, d := range diags {
			if d.Rule == RuleParseError && strings.Contains(d.Message, tc.want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: no diagnostic mentioning %q in %v", tc.src, tc.want, diags)
		}
	}
}

func TestParseValueSrcKeepsInteriorTrivia(t *testing.T) {
	r, _ := parseOne(t, ".a{grid-template-areas:\n  \"summary summary\"\n  \"files   main\";\n}")
	// ValueSrc is the raw source slice, outer whitespace trimmed; the
	// newline and indent between the strings survive verbatim.
	if r.Decls[0].ValueSrc != "\"summary summary\"\n  \"files   main\"" {
	}
}

func TestParsePositions(t *testing.T) {
	src := "\n\n.card .fui-card__body { gap: 0; }"
	sheet, diags := Parse(src)
	if len(diags) != 0 || len(sheet.Rules) != 1 {
		t.Fatalf("diags %v rules %d", diags, len(sheet.Rules))
	}
	r := sheet.Rules[0]
	if r.Pos != (Pos{3, 1}) {
		t.Errorf("rule pos: %+v", r.Pos)
	}
	if r.Decls[0].Pos != (Pos{3, 25}) {
		t.Errorf("decl pos: %+v", r.Decls[0].Pos)
	}
}

func TestParseDeclAfterNestedRuleIsError(t *testing.T) {
	// Declations after a nested rule are valid CSS (a
	// CSSNestedDeclarations rule at their position) but this package
	// stores Decls and Nested apart, which would hoist them above the
	// nested rule and flip the cascade. That is a parse error: move
	// the declaration up.
	src := ".a { & { color: var(--color-primary); } color: var(--color-text); }"
	sheet, diags := Parse(src)
	if len(diags) != 1 || diags[0].Rule != RuleParseError {
		t.Fatalf("want one parse error, got %v", diags)
	}
	if !strings.Contains(diags[0].Message, "declaration after a nested rule") {
		t.Fatalf("message: %s", diags[0].Message)
	}
	// Position: the declaration's name token (column 41).
	if diags[0].Line != 1 || diags[0].Col != 41 {
		t.Fatalf("position: %d:%d", diags[0].Line, diags[0].Col)
	}
	// Recovery keeps parsing: the decl is still recorded for checks.
	if len(sheet.Rules) != 1 || len(sheet.Rules[0].Decls) != 1 || len(sheet.Rules[0].Nested) != 1 {
		t.Fatalf("recovery lost the rule: %+v", sheet.Rules[0])
	}
	// The clean order reports nothing.
	if _, diags := Parse(".a { color: red; & { color: blue; } }"); len(diags) != 0 {
		t.Fatalf("clean order flagged: %v", diags)
	}
}
