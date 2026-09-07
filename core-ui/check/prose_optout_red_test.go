//go:build red

package check

// RED TEST — open finding, 2026-09-06/07 adversarial round 5 (fringe wave; tier T2).
//
// Property: a check-csp opt-out is live only when the directive starts its
// own comment — prose or documentation merely QUOTING the directive never
// exempts a file. This is the house grammar, stated and implemented at
// framework/contracts/suppress.go:24-35: "The directive must be the FIRST
// thing in its comment. That anchor is what separates a live suppression
// from documentation *about* suppressions."
//
// Surfaces: core-ui/check/noinlinescripts.go::hasCSPIgnoreDirective
// (:23-27) is a whole-file strings.Contains, so any file whose prose
// mentions the directive silently opts out of BOTH gates: LintNoInlineStyles
// (+Recursive) and LintNoInlineScripts(+Recursive) / ScanInlineScriptsIn
// (:98-100, the contracts entry), the cmd/check-csp build gate, and the
// contracts GOFASTR1804/1805 via
// framework/contracts/analyzers/rendering.go:791-794 — whose own comment at
// :793 ("continue // file opted out with //check-csp:ignore-file") is the
// in-tree demonstration: rendering.go silently exempts itself from the
// inline-script contract today.
//
// Finding: a doc comment (or string literal) quoting the directive as
// prose — "// docs: opt a file out with // check-csp:ignore-file at the
// top" — suppresses every inline-style and inline-script violation in
// that file. Both fixtures below pass clean today.
//
// Fix direction: anchor the directive the way suppress.go:35 does — a
// live opt-out must be comment-initial (^//\s*check-csp:ignore-file at
// the start of a comment line), not a substring anywhere in the file.
// Pinned siblings kept as guards: TestLintNoInlineStyles_
// HonorsIgnoreDirective and TestLintNoInlineScripts_HonorsIgnoreDirective
// (a genuine top-of-file directive still suppresses).

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// proseMention is a doc comment that merely documents the directive: the
// marker is quoted mid-sentence, never comment-initial.
const proseMention = "// docs: opt a file out with // check-csp:ignore-file at the top"

// TestProseMentionRedNotOptOut: quoting the opt-out directive in prose
// must not disable either CSP gate. Guards prove the gates still fire
// without any mention, and that a genuine comment-initial directive still
// suppresses (existing pin semantics).
func TestProseMentionRedNotOptOut(t *testing.T) {
	// ── Arm 1: inline-style gate (LintNoInlineStyles, the cmd/check-csp
	// surface). Prose mention + an inline style emission must VIOLATE. ──
	styleDir := t.TempDir()
	writeFixture(t, styleDir, "fixture.go", proseMention+`
package fixture

var x = "<div style=\"color:red\">hi</div>"
`)
	res, err := LintNoInlineStyles(styleDir)
	if err != nil {
		t.Fatalf("setup broken: LintNoInlineStyles: %v", err)
	}
	if len(res.Violations) == 0 {
		t.Errorf("SECURITY: [checkcsp-prose-optout] a doc comment QUOTING '// check-csp:ignore-file' mid-sentence disabled the inline-style gate for the whole file — hasCSPIgnoreDirective (noinlinescripts.go:23-27) is a whole-file strings.Contains, so prose opts the file out exactly like a live directive; the fixture emits style=\"color:red\" and must be flagged (suppress.go:24-35 grammar: quoted forms are inert)")
	}

	// Guard: with no mention at all, the gate fires (calibration).
	plainDir := t.TempDir()
	writeFixture(t, plainDir, "fixture.go", `package fixture

var x = "<div style=\"color:red\">hi</div>"
`)
	pres, err := LintNoInlineStyles(plainDir)
	if err != nil {
		t.Fatalf("setup broken: LintNoInlineStyles (control): %v", err)
	}
	if len(pres.Violations) == 0 {
		t.Fatal("setup broken: control fixture with no directive mention produced no inline-style violations — gate calibration failed")
	}

	// Guard: a genuine comment-initial directive still suppresses
	// (TestLintNoInlineStyles_HonorsIgnoreDirective semantics).
	liveDir := t.TempDir()
	writeFixture(t, liveDir, "fixture.go", `//check-csp:ignore-file
package fixture

var x = "<div style=\"color:red\">hi</div>"
`)
	lres, err := LintNoInlineStyles(liveDir)
	if err != nil {
		t.Fatalf("setup broken: LintNoInlineStyles (live directive): %v", err)
	}
	if len(lres.Violations) != 0 {
		t.Errorf("SECURITY: [checkcsp-prose-optout] genuine top-of-file '//check-csp:ignore-file' no longer suppresses (got %d violations) — the anchor fix must keep the documented escape hatch live", len(lres.Violations))
	}

	// ── Arm 2: inline-script gate via ScanInlineScriptsIn — the exact
	// entry the contracts pass (GOFASTR1804, rendering.go:791) uses. ──
	scriptSrc := proseMention + `
package fixture

var s = "<script>alert(1)</script>"
`
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "fixture.go")
	writeFixture(t, scriptDir, "fixture.go", scriptSrc)
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, scriptPath, []byte(scriptSrc), parser.AllErrors|parser.ParseComments)
	if err != nil {
		t.Fatalf("setup broken: parse fixture: %v", err)
	}
	sres := ScanInlineScriptsIn(fset, file, scriptPath, []byte(scriptSrc))
	if sres == nil {
		t.Errorf("SECURITY: [checkcsp-prose-optout] ScanInlineScriptsIn returned nil (exempt) for a file whose only 'directive' is prose quoting '// check-csp:ignore-file' mid-sentence — the contracts pass (rendering.go:791-794) then skips the file, so a doc comment disables GOFASTR1804; the fixture's '<script>alert(1)</script>' literal must be scanned")
	} else if len(sres.Violations) == 0 {
		t.Errorf("SECURITY: [checkcsp-prose-optout] ScanInlineScriptsIn scanned the prose-mention fixture but reported 0 violations for a literal containing '<script>alert(1)</script>' — the inline-script gate is silently off for this file")
	}

	// Guard: no mention → the scan is live and flags the literal.
	plainSrc := `package fixture

var s = "<script>alert(1)</script>"
`
	plainSDir := t.TempDir()
	plainSPath := filepath.Join(plainSDir, "fixture.go")
	writeFixture(t, plainSDir, "fixture.go", plainSrc)
	pfset := token.NewFileSet()
	pfile, err := parser.ParseFile(pfset, plainSPath, []byte(plainSrc), parser.AllErrors|parser.ParseComments)
	if err != nil {
		t.Fatalf("setup broken: parse control fixture: %v", err)
	}
	pres2 := ScanInlineScriptsIn(pfset, pfile, plainSPath, []byte(plainSrc))
	if pres2 == nil || len(pres2.Violations) == 0 {
		t.Fatal("setup broken: control fixture with no directive mention produced no inline-script violations — gate calibration failed")
	}

	// Guard: a genuine comment-initial directive still returns nil
	// (ScanInlineScriptsIn's documented opt-out contract).
	liveSrc := `//check-csp:ignore-file
package fixture

var s = "<script>alert(1)</script>"
`
	liveSDir := t.TempDir()
	liveSPath := filepath.Join(liveSDir, "fixture.go")
	writeFixture(t, liveSDir, "fixture.go", liveSrc)
	lfset := token.NewFileSet()
	lfile, err := parser.ParseFile(lfset, liveSPath, []byte(liveSrc), parser.AllErrors|parser.ParseComments)
	if err != nil {
		t.Fatalf("setup broken: parse live-directive fixture: %v", err)
	}
	if lres2 := ScanInlineScriptsIn(lfset, lfile, liveSPath, []byte(liveSrc)); lres2 != nil {
		t.Errorf("SECURITY: [checkcsp-prose-optout] genuine top-of-file '//check-csp:ignore-file' no longer exempts (ScanInlineScriptsIn returned non-nil) — the anchor fix must keep the documented escape hatch live")
	}
}
