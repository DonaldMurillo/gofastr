package ownstyle

import (
	"strings"
	"testing"
)

func TestSuppressedHonoursMarkers(t *testing.T) {
	src := `.a {
  /* gofastr:allow(GOFASTR1806) the embed host sets --host-ink */
  color: var(--host-ink);
  background: var(--host-bg); /* gofastr:allow(GOFASTR1806) set by the embed host */
  border-color: var(--host-line);
  /* gofastr:allow(GOFASTR1806) */
  outline-color: var(--host-ring);
  /* gofastr:allow(GOFASTR1807) wrong rule */
  fill: var(--host-fill);
}
`
	diags := Check("t.style.css", src, KindScoped, defaultTokens(t))
	kept := map[string]bool{}
	for _, d := range diags {
		if d.Rule == RuleUnknownThemeToken && !Suppressed(src, d) {
			kept[strings.Fields(d.Message)[0]] = true
		}
	}
	for name, want := range map[string]bool{
		"--host-ink":  false, // marker on the line above
		"--host-bg":   false, // trailing marker
		"--host-line": true,  // no marker
		"--host-ring": true,  // marker without a reason silences nothing
		"--host-fill": true,  // marker for another rule
	} {
		if kept[name] != want {
			t.Errorf("%s kept = %v, want %v (kept: %v)", name, kept[name], want, kept)
		}
	}
}

// repeatedLiteralFindings flattens RepeatedLiteralsIn (what both
// verify and gen styles consume per program) for the assertions below.
func repeatedLiteralFindings(sheets []SheetSource, tokens map[string]string) []FileDiagnostic {
	var out []FileDiagnostic
	for _, r := range RepeatedLiteralsIn(sheets, tokens) {
		out = append(out, r.FileDiagnostic())
	}
	return out
}

func TestRepeatedLiterals(t *testing.T) {
	sheets := []SheetSource{
		{File: "a/board.style.css", Src: ".x { color: #0F766E; max-width: 37rem; padding: 10px; }"},
		{File: "b/card.style.css", Src: ".y { background: #0f766e; width: 37rem; }\n.z { padding: 10px; }"},
		{File: "c/note.style.css", Src: ".w { color: #4F46E5; border-radius: 3px; }"},
		{File: "c/tag.style.css", Src: ".v { border-radius: 3px; }"},
	}
	tokens := map[string]string{"color-primary": "#4F46E5"}
	got := repeatedLiteralFindings(sheets, tokens)
	var lines []string
	for _, d := range got {
		lines = append(lines, d.File+": "+d.Diag.Message)
	}
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"a/board.style.css: #0F766E is also written in b/card.style.css",
		"b/card.style.css: #0f766e is also written in a/board.style.css",
		"a/board.style.css: 37rem is also written in b/card.style.css",
		"a/board.style.css: 10px is also written in b/card.style.css",
		"c/note.style.css: 3px is also written in c/tag.style.css",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	// #4F46E5 is a token's value: that is GOFASTR1807's finding.
	if strings.Contains(joined, "#4F46E5") {
		t.Errorf("a literal equal to a token is 1807's, not 1822's:\n%s", joined)
	}
	for _, d := range got {
		if d.Diag.Severity != SeverityWarning || d.Diag.Rule != RuleRepeatedLiteral {
			t.Errorf("want warn %s, got %s %s", RuleRepeatedLiteral, d.Diag.Severity, d.Diag.Rule)
		}
	}
}

// Zeros, a 100% fill and a -1 z-index say "none", "fill" or "behind",
// not a size: two sheets resetting a list share no look. A z-index
// other than -1 and a non-zero length still count.
func TestRepeatedStructuralValuesPass(t *testing.T) {
	sheets := []SheetSource{
		{File: "a/x.style.css", Src: ".l { margin: 0; padding: 0 0; inline-size: 100%; z-index: -1; }\n.m { z-index: 5; margin: 0 1px; }"},
		{File: "b/y.style.css", Src: ".l { margin: 0px; padding: 0; inline-size: 100%; z-index: -1; }\n.m { z-index: 5; margin: 0 1px; }"},
	}
	var msgs []string
	for _, d := range repeatedLiteralFindings(sheets, nil) {
		msgs = append(msgs, d.Diag.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, quiet := range []string{"0 is also", "0px is also", "0 0 is also", "100% is also", "-1 is also"} {
		if strings.Contains(joined, quiet) {
			t.Errorf("structural value reported (%q):\n%s", quiet, joined)
		}
	}
	for _, loud := range []string{"5 is also", "0 1px is also"} {
		if !strings.Contains(joined, loud) {
			t.Errorf("missing %q in\n%s", loud, joined)
		}
	}
}

func TestCheckTokenFiles(t *testing.T) {
	builtins := map[string]string{"color-primary": "#4F46E5"}
	files := []SheetSource{
		{File: "a/acme.tokens.css", Src: `@property --color-primary { syntax: "<color>"; inherits: true; initial-value: #000; }
@property --size-rail { syntax: "<length>"; inherits: true; initial-value: 18rem; }`},
		{File: "b/more.tokens.css", Src: `@property --size-rail { syntax: "<length>"; inherits: true; initial-value: 20rem; }
@property --color-ink { syntax: "<color>"; inherits: true; initial-value: #111; }
@media (--dark) { :root { --color-ink: #eee; } }`},
	}
	_, tokens, diags := CheckTokenFiles(files, builtins)
	var msgs []string
	for _, d := range diags {
		msgs = append(msgs, d.File+": "+d.Diag.Message)
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{
		"a/acme.tokens.css: --color-primary is a built-in theme token",
		"b/more.tokens.css: --size-rail is already declared in a/acme.tokens.css",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in\n%s", want, joined)
		}
	}
	m := CheckTokens(builtins, tokens)
	if m["size-rail"] != "18rem" || m["color-ink"] != "#111" || m["dark.color-ink"] != "#eee" || m["color-primary"] != "#4F46E5" {
		t.Errorf("check map = %v", m)
	}
}
