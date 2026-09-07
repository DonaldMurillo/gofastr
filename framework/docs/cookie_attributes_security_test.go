package docs

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// Pins: every Set-Cookie mint site the DOCS teach emits the attributes
// its threat model requires (HttpOnly, Secure, SameSite — GOFASTR1404,
// held over compile-marked snippets the way ruleInsecureCookie holds
// every .go mint site).
// Family: GOFASTR1404, "cookie is missing HttpOnly, Secure, SameSite" — the
// contract pinned for real .go code by framework/contracts/analyzers/
// security.go::ruleInsecureCookie, with its acceptance keys pinned in
// framework/contracts/analyzers/analyzers_test.go:359 ("HttpOnly: true",
// "Secure:   true", "SameSite: http.SameSiteLaxMode") and its catalog Good
// example (framework/contracts/catalog.go:645) spelling out all three
// attributes on the fixed literal. The repo's own mint sites either carry
// the attributes or a //gofastr:allow(GOFASTR1404) reason
// (core/middleware/csrf.go:231, examples/backoffice/main.go:221). The
// analyzer scans .go only.
// Property: every Set-Cookie mint site the DOCS teach emits the attributes
// its threat model requires. Compile-marked snippets are compiled
// (TestDocExamplesCompile) but never analyzed, so the canonical recipes
// escape the family contract — the exact escape this family enumeration
// hunts.
// Surfaces: framework/docs/content/i18n.md:184 — the canonical
// "change language" cookie (fixed 2026-09-07: the recipe used to set
// only Name/Value/Path from raw r.FormValue("lang"), in the doc that
// two paragraphs earlier calls resolver values "attacker-controlled",
// i18n.md:154).
// Finding (pre-fix): apps copying the canonical locale handler minted an
// attribute-less cookie — readable by any injected script (no
// HttpOnly), traveling over plain HTTP after a downgrade (no Secure),
// riding cross-site requests (no SameSite).
// Fix direction: teach the attributes in the snippet (HttpOnly: true,
// Secure: true, SameSite: http.SameSiteStrictMode — a locale preference
// needs no script access) or annotate the SetCookie line with a
// //gofastr:allow(GOFASTR1404) reason, and keep a corpus-side GOFASTR1404
// check over compile-marked snippets so the docs cannot drift from the
// .go contract again.

// redCookieWant are the attributes GOFASTR1404 demands of every cookie
// carrying a value.
var redCookieWant = []string{"HttpOnly", "Secure", "SameSite"}

var (
	redCookieHasKey    = map[string]*regexp.Regexp{}
	redCookieEmptyVal  = regexp.MustCompile(`\bValue:\s*""`)
	redCookieNegMaxAge = regexp.MustCompile(`\bMaxAge:\s*-\d`)
)

func init() {
	for _, k := range append([]string{"Value"}, redCookieWant...) {
		redCookieHasKey[k] = regexp.MustCompile(`\b` + k + `\s*:`)
	}
}

// redCookieFinding is one insecure literal: line is 1-based within the
// snippet; missing names the absent attribute keys.
type redCookieFinding struct {
	line    int
	missing []string
}

// redCookieLiteralLines collects the snippet lines of the composite literal
// opening on lines[start] by brace matching. Doc snippets carry no braces
// inside string values, so counting is exact.
func redCookieLiteralLines(lines []string, start int) []string {
	depth := 0
	for j := start; j < len(lines) && j < start+40; j++ {
		depth += strings.Count(lines[j], "{") - strings.Count(lines[j], "}")
		if depth <= 0 {
			return lines[start : j+1]
		}
	}
	return lines[start:]
}

// redCookieScan is the textual mirror of ruleInsecureCookie: it finds every
// http.Cookie composite literal in a Go snippet and reports the ones missing
// the attribute set, honoring the analyzer's two exemptions — the deletion
// shape (no Value key, or the standard delete of empty Value + negative
// MaxAge) and a //gofastr:allow(GOFASTR1404) comment on the line directly
// above (the csrf.go:231 precedent).
func redCookieScan(snippet string) []redCookieFinding {
	lines := strings.Split(snippet, "\n")
	var out []redCookieFinding
	for i, line := range lines {
		if !strings.Contains(line, "http.Cookie{") {
			continue
		}
		body := strings.Join(redCookieLiteralLines(lines, i), "\n")
		if !redCookieHasKey["Value"].MatchString(body) ||
			(redCookieEmptyVal.MatchString(body) && redCookieNegMaxAge.MatchString(body)) {
			continue // deletion shape: nothing carried, nothing to protect
		}
		if i > 0 && strings.Contains(lines[i-1], "gofastr:allow(GOFASTR1404)") {
			continue // line-anchored, reasoned suppression
		}
		var missing []string
		for _, k := range redCookieWant {
			if !redCookieHasKey[k].MatchString(body) {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			out = append(out, redCookieFinding{line: i + 1, missing: missing})
		}
	}
	return out
}

// TestDocsCookieAttributesTaught runs the GOFASTR1404 check over every
// compile-marked snippet in the docs corpus — the snippets the repo already
// promises compile — and fails on any cookie mint the .go analyzer would
// have flagged.
func TestDocsCookieAttributesTaught(t *testing.T) {
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
			if !strings.Contains(snippet, "http.Cookie{") {
				continue
			}
			marked++
			// Line 1 of the snippet body within the doc: the fence's
			// opening newline has already been counted.
			baseLine := strings.Count(text[:m[4]], "\n") + 1
			for _, f := range redCookieScan(snippet) {
				t.Errorf("SECURITY: [docs-cookie-attributes] %s:%d compile-marked snippet mints an http.Cookie missing %s — apps copying the canonical recipe inherit an attribute-less cookie; ruleInsecureCookie holds every .go mint site to these keys and the analyzer never sees this snippet",
					e.Name(), baseLine+f.line-1, strings.Join(f.missing, ", "))
			}
		}
	}
	if marked == 0 {
		t.Fatalf("setup broken: no compile-marked snippet in the docs corpus contains http.Cookie{ — the i18n locale recipe moved; re-point this test")
	}

	// Positive controls: the checker's pass-paths, mirroring the analyzer's
	// exemption grammar so the gate stays honest once the corpus is fixed.
	if f := redCookieScan("http.SetCookie(w, &http.Cookie{\n    Name: \"locale\", Value: lang, Path: \"/\",\n    HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,\n})"); len(f) != 0 {
		t.Fatalf("setup broken: checker flags a compliant literal (catalog.go Good example shape): %+v", f)
	}
	if f := redCookieScan("http.SetCookie(w, &http.Cookie{Name: \"locale\", Value: \"\", MaxAge: -1, Path: \"/\"})"); len(f) != 0 {
		t.Fatalf("setup broken: checker flags the deletion shape the analyzer exempts: %+v", f)
	}
	if f := redCookieScan("//gofastr:allow(GOFASTR1404) locale cookie is deliberately script-readable\nhttp.SetCookie(w, &http.Cookie{Name: \"locale\", Value: lang})"); len(f) != 0 {
		t.Fatalf("setup broken: checker ignores the line-anchored allow reason: %+v", f)
	}
	// And its fire-path: a bare literal IS flagged, or the corpus gate
	// above would pass vacuously.
	if f := redCookieScan("http.SetCookie(w, &http.Cookie{Name: \"locale\", Value: lang})"); len(f) == 0 {
		t.Fatalf("setup broken: checker does not flag a bare literal — the corpus gate would pass vacuously")
	}
}
