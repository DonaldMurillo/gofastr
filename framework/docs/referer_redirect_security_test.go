package docs

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Pins: no compile-marked docs snippet redirects to a bare
// r.Referer()/req.Referer() value — the Referer header is
// attacker-controlled, and the canonical recipes apps copy verbatim
// are held to the repo's own redirect hygiene (battery/auth
// safeReferer / isSafeRelativePath grammar).
// CONTRACT ANSWER: yes — a canonical recipe is docs-teaching grade.
// Property: no compile-marked docs snippet redirects to a bare
// r.Referer()/req.Referer() value. The Referer header is attacker-
// controlled (a cross-site POST carries the attacker's page), so
// http.Redirect(w, r, r.Referer(), 303) is a textbook open redirect; the
// battery solves the exact same "return to where you came from" need by
// resolving the referer through safeReferer/isSafeRelativePath first.
// Surfaces: content/i18n.md:192 — the canonical "change language" handler
// ends with http.Redirect(w, r, backToSender(r), http.StatusSeeOther):
// the wrapped target is the accepted form, and this corpus-side check
// keeps it that way. The snippet compiles (TestDocExamplesCompile);
// before the 2026-09-07 fix the same line read
// http.Redirect(w, r, r.Referer(), …), verbatim raw-header redirect.
// Fix direction: teach the safe form in the snippet — resolve through a
// same-origin/safe-relative helper (safeReferer/isSafeRelativePath shape)
// or fall back to a relative literal — and keep this corpus-side check so
// the docs cannot teach raw-header redirects again.

// refererRedirectRe matches an http.Redirect whose target argument is a
// bare (r|req|request|httpReq).Referer() call. Wrapped targets —
// safeReferer(r), an isSafeRelativePath-guarded variable, or a relative
// literal — do not match, mirroring the accepted forms of the battery's
// own handlers.
var refererRedirectRe = regexp.MustCompile(
	`\bhttp\.Redirect\(\s*\w+\s*,\s*\w+\s*,\s*(?:r|req|request|httpReq)\.Referer\(\)\s*,`)

// refererFindings returns the 1-based snippet lines of every bare
// Referer redirect in a compile-marked snippet.
func refererFindings(snippet string) []int {
	lines := strings.Split(snippet, "\n")
	var out []int
	for i, line := range lines {
		if refererRedirectRe.MatchString(line) {
			out = append(out, i+1)
		}
	}
	return out
}

// TestDocsRefererNotTaught runs the referer-redirect check over every
// compile-marked snippet in the docs corpus — the snippets the repo
// already promises compile — and fails on any bare Referer redirect the
// battery's own handlers would never emit.
func TestDocsRefererNotTaught(t *testing.T) {
	entries, err := fs.ReadDir(contentFS, "content")
	if err != nil {
		t.Fatalf("setup broken: read content dir: %v", err)
	}
	marked := 0
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		body, err := fs.ReadFile(contentFS, "content/"+e.Name())
		if err != nil {
			t.Fatalf("setup broken: read %s: %v", e.Name(), err)
		}
		text := string(body)
		for _, m := range compileDirective.FindAllStringSubmatchIndex(text, -1) {
			snippet := text[m[4]:m[5]]
			if !strings.Contains(snippet, "http.Redirect(") {
				continue
			}
			marked++
			// Line 1 of the snippet body within the doc: the fence's
			// opening newline has already been counted.
			baseLine := strings.Count(text[:m[4]], "\n") + 1
			for _, line := range refererFindings(snippet) {
				t.Errorf("SECURITY: [docs-referer-redirect] %s:%d compile-marked snippet redirects to a bare r.Referer() — "+
					"the Referer header is attacker-controlled, so the canonical recipe teaches a textbook open redirect; "+
					"the battery's own handlers resolve the same value through safeReferer/isSafeRelativePath before redirecting",
					e.Name(), baseLine+line-1)
			}
		}
	}
	if marked == 0 {
		t.Fatalf("setup broken: no compile-marked snippet in the docs corpus contains http.Redirect( — the i18n locale recipe moved; re-point this test")
	}

	// Positive controls: the checker's pass-paths, so the gate stays
	// honest once the corpus is fixed.
	if f := refererFindings("http.Redirect(w, r, safeReferer(r), http.StatusSeeOther)"); len(f) != 0 {
		t.Fatalf("setup broken: checker flags a safeReferer-wrapped redirect: %v", f)
	}
	if f := refererFindings("if !isSafeRelativePath(ref) {\n    ref = \"/\"\n}\nhttp.Redirect(w, r, ref, http.StatusSeeOther)"); len(f) != 0 {
		t.Fatalf("setup broken: checker flags an isSafeRelativePath-guarded redirect: %v", f)
	}
	if f := refererFindings("http.Redirect(w, r, \"/locale\", http.StatusSeeOther)"); len(f) != 0 {
		t.Fatalf("setup broken: checker flags a relative-literal redirect: %v", f)
	}
	// And its fire-path: a bare Referer redirect IS flagged, or the
	// corpus gate above would pass vacuously.
	if f := refererFindings("http.Redirect(w, r, r.Referer(), http.StatusSeeOther)"); len(f) == 0 {
		t.Fatalf("setup broken: checker does not flag a bare Referer redirect — the corpus gate would pass vacuously")
	}
}
