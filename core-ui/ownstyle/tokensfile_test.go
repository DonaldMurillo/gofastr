package ownstyle

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

const acmeTokens = `/* The column the header, main and footer share. */
@property --size-page-width { syntax: "<length>"; inherits: true; initial-value: 67.5rem; }
@property --color-highlight { syntax: "<color>"; inherits: true; initial-value: #0F766E; }
@property --font-weight-display { syntax: "<number>"; inherits: true; initial-value: 800; }
@property --duration-unroll { syntax: "<time>"; inherits: true; initial-value: 260ms; }
@property --easing-unroll { syntax: "*"; inherits: true; initial-value: cubic-bezier(0.2, 0, 0, 1); }

@media (--dark) {
  :root { --color-highlight: #5EEAD4; }
}
`

func TestParseTokensReadsEveryKind(t *testing.T) {
	f, diags := ParseTokens(acmeTokens)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	want := map[string]any{
		"size-page-width":     style.Size{Name: "page-width", Value: "67.5rem"},
		"color-highlight":     style.Color{Name: "highlight", Value: "#0F766E"},
		"font-weight-display": style.FontWeight{Name: "display", Value: 800},
		"easing-unroll":       style.Easing{Name: "unroll", Value: "cubic-bezier(0.2, 0, 0, 1)"},
	}
	got := map[string]AppToken{}
	for _, tk := range f.Tokens {
		got[tk.Key] = tk
	}
	for k, v := range want {
		if got[k].Value != v {
			t.Errorf("%s = %#v, want %#v", k, got[k].Value, v)
		}
	}
	if got["color-highlight"].Dark != "#5EEAD4" {
		t.Errorf("dark value = %q", got["color-highlight"].Dark)
	}
	if got["size-page-width"].Doc != "The column the header, main and footer share." {
		t.Errorf("doc = %q", got["size-page-width"].Doc)
	}
}

func TestParseTokensRefuses(t *testing.T) {
	for name, tc := range map[string]struct{ src, want string }{
		"syntax mismatch": {
			`@property --size-x { syntax: "<color>"; inherits: true; initial-value: red; }`,
			`--size-x is a Size token: its syntax is "<length>"`,
		},
		"unknown prefix": {
			`@property --brand-x { syntax: "<color>"; inherits: true; initial-value: red; }`,
			"starts with a token type",
		},
		"bad value": {
			`@property --size-x { syntax: "<length>"; inherits: true; initial-value: huge; }`,
			"--size-x",
		},
		"not inherited": {
			`@property --size-x { syntax: "<length>"; inherits: false; initial-value: 1rem; }`,
			"inherits: true",
		},
		"missing value": {
			`@property --size-x { syntax: "<length>"; inherits: true; }`,
			"initial-value",
		},
		"twice": {
			`@property --size-x { syntax: "<length>"; inherits: true; initial-value: 1rem; }
@property --size-x { syntax: "<length>"; inherits: true; initial-value: 2rem; }`,
			"declared twice",
		},
		"stray rule": {
			`.card { color: red; }`,
			"only @property",
		},
		"dark non-colour": {
			`@property --size-x { syntax: "<length>"; inherits: true; initial-value: 1rem; }
@media (--dark) { :root { --size-x: 2rem; } }`,
			"only colours",
		},
		"dark undeclared": {
			`@media (--dark) { :root { --color-ink: #fff; } }`,
			"not declared",
		},
		"other media": {
			`@media (min-width: 40rem) { :root { --color-ink: #fff; } }`,
			"(--dark)",
		},
		"unknown descriptor": {
			`@property --size-x { syntax: "<length>"; inherits: true; initial-value: 1rem; color: red; }`,
			"descriptor",
		},
	} {
		_, diags := ParseTokens(tc.src)
		if !hasDiag(diags, tc.want) {
			t.Errorf("%s: want a diagnostic containing %q, got %v", name, tc.want, diags)
		}
	}
}

func hasDiag(diags []Diagnostic, want string) bool {
	for _, d := range diags {
		if d.Severity == SeverityError && strings.Contains(d.Message, want) {
			return true
		}
	}
	return false
}

func TestGenerateTokensFile(t *testing.T) {
	f, diags := ParseTokens(acmeTokens)
	if len(diags) != 0 {
		t.Fatal(diags)
	}
	out, err := GenerateTokensFile("acme", acmeTokens, f, "acme", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		GeneratedByHeader + "acme.tokens.css. DO NOT EDIT.",
		SourceHashHeader + SourceHash(acmeTokens),
		"package acme",
		"var Tokens = acmeTokens{",
		`PageWidth: style.Size{Name: "page-width", Value: "67.5rem"},`,
		`Highlight: style.Color{Name: "highlight", Value: "#0F766E"},`,
		`Display: style.FontWeight{Name: "display", Value: 800},`,
		`Unroll: style.Duration{Name: "unroll", Value: 260 * time.Millisecond},`,
		"// PageWidth is --size-page-width. The column the header, main and footer share.",
		`"highlight": "#5EEAD4",`,
		"func (acmeTokens) DarkTokens() map[string]string {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("generated file is missing %q\n%s", want, out)
		}
	}
	shared, err := GenerateTokensFile("acme", acmeTokens, f, "acme", true)
	if err != nil || !strings.Contains(shared, "var AcmeTokens = acmeTokens{") {
		t.Errorf("a package with several token files names the var after the file: %v", err)
	}
}

func TestDuplicateTokenValues(t *testing.T) {
	builtins := map[string]string{"color-primary": "#4F46E5", "size-header-height": "56px"}
	app := []AppTokenAt{
		{File: "a.tokens.css", Token: AppToken{Key: "color-brand", Light: "#4f46e5", Pos: Pos{1, 1}}},
		{File: "a.tokens.css", Token: AppToken{Key: "size-rail", Light: "18rem", Pos: Pos{2, 1}}},
		{File: "b.tokens.css", Token: AppToken{Key: "size-drawer", Light: "18rem", Pos: Pos{1, 1}}},
		{File: "b.tokens.css", Token: AppToken{Key: "spacing-bar", Light: "56px", Pos: Pos{2, 1}}},
	}
	got := DuplicateTokenValues(app, builtins)
	var msgs []string
	for _, d := range got {
		msgs = append(msgs, d.File+": "+d.Diag.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		"a.tokens.css: --color-brand has the value of --color-primary",
		"b.tokens.css: --size-drawer has the value of --size-rail",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "spacing-bar") {
		t.Errorf("a spacing token equal to a size token is a different kind of value:\n%s", joined)
	}
	if len(got) != 2 {
		t.Errorf("want 2 findings, got %d:\n%s", len(got), joined)
	}
}
