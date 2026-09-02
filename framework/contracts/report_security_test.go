package contracts

import (
	"os"
	"path/filepath"
	"testing"
)

// Property: no raw C0/DEL byte from analysed repo content reaches the
// rendered text report. Diagnostics embed repo-derived strings (the
// rule reference of a malformed directive, the source-line snippet, the
// file's own name), and FormatText prints Message, Snippet and Location
// verbatim. An ESC byte in any of them is terminal-escape injection
// into the operator running `gofastr verify` on a hostile PR; NUL/VT
// corrupt terminal framing the same way.
func TestReportTextFreeOfControlBytes(t *testing.T) {
	_, sup := probePass(t, map[string]string{
		// ESC inside the rule reference: the directive is parsed from the
		// comment, the reference fails catalog lookup, and both the
		// message and the snippet carry the raw ESC.
		"esc.go": "package a\n\n//gofastr:allow(GOFASTR1403\x1b[31mFAKE) reviewing\nfunc f() {}\n",
		// DEL in the reference.
		"del.go": "package a\n\n//gofastr:allow(GOFASTR1403\x7f) reviewing\nfunc f() {}\n",
		// NUL makes the file unparsable; the line-scan fallback still
		// finds the directive, so the NUL must not survive to the report.
		"nul.go": "package a\n\n//gofastr:allow(GOFASTR1403\x00) reviewing\nfunc f() {}\n",
		// Vertical tab: legal inside a Go comment.
		"vt.go": "package a\n\n//gofastr:allow(GOFASTR1403\x0b) reviewing\nfunc f() {}\n",
		// The file's own name carries the ESC; Location renders it raw.
		"ev\x1bil.go": "//gofastr:allow(GOFASTR7777) typo\npackage a\n",
	})
	if len(sup.issues) == 0 {
		t.Fatal("premise failed: the fixtures produced no suppression meta-diagnostics")
	}
	out := FormatText(&Report{Diagnostics: sup.issues}, TextOptions{})
	for i, b := range []byte(out) {
		if (b < 0x20 && b != '\n' && b != '\t') || b == 0x7f {
			lo, hi := max(0, i-40), min(len(out), i+40)
			t.Fatalf("SECURITY: [term-inject] byte %#02x from repo content reached the rendered report at offset %d: %q", b, i, out[lo:hi])
		}
	}
}

// Apply is the one function in the package that writes to disk, and
// contracts_fix exposes it over MCP. containedPath is lexical only: a
// symlink inside the analysed tree that points outside the root passes
// it, and the fix is written through to the target.
func TestApplyRefusesSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.go")
	const orig = "//gofastr:allow(GOFASTR1403) stale\npackage a\n"
	if err := os.WriteFile(victim, []byte(orig), 0o644); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Symlink(victim, filepath.Join(root, "stale.go")); err != nil {
		t.Skipf("symlink creation refused: %v", err)
	}

	rep := &Report{Root: root, Diagnostics: []Diagnostic{{
		RuleID: RuleSuppressionStale, File: "stale.go", Line: 1,
		Fix: &SuggestedFix{
			Description: "delete stale directive",
			Edits:       []TextEdit{{Start: 0, End: len(orig), Old: orig, New: ""}},
		},
	}}}
	_, err := rep.Apply()
	body, readErr := os.ReadFile(victim)
	if err == nil && readErr == nil && string(body) != orig {
		t.Fatalf("SECURITY: [path-escape] Apply wrote through root/stale.go to %s outside the analysed root", victim)
	}
}

// The lexical guard itself: traversal and absolute diagnostic paths are
// refused before any read or write happens.
func TestApplyRefusesTraversalAndAbs(t *testing.T) {
	dir := t.TempDir()
	for _, rel := range []string{"../evil.go", "a/../../evil.go", "/etc/passwd", ".."} {
		rep := &Report{Root: dir, Diagnostics: []Diagnostic{{
			RuleID: "GOFASTR0002", File: rel, Line: 1,
			Fix: &SuggestedFix{Description: "x", Edits: []TextEdit{{Start: 0, End: 0, New: "x"}}},
		}}}
		if _, err := rep.Apply(); err == nil {
			t.Errorf("SECURITY: [path-escape] File %q was accepted for writing", rel)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "evil.go")); err == nil {
		t.Error("SECURITY: [path-escape] a file appeared next to the root despite refused paths")
	}
}
