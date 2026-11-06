package a

import (
	"bytes"
	"regexp"
	"strings"
)

var (
	scriptTagRe       = regexp.MustCompile(`(?is)<script\b[^>]*>.*?</script\s*>`)
	autofocusAttrRe   = regexp.MustCompile(`(?i)\s*autofocus\b`)
	ctlRe             = regexp.MustCompile(`[\x00-\x1f]+`)
	anyTagRe          = regexp.MustCompile(`<[^>]+>`)
	narrowTagRe       = regexp.MustCompile(`<[^<>]+>`)
	ansiRe            = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
	unquoteIdentRe    = regexp.MustCompile(`"([a-z_]+)"`)
	foldedDelimitedRe = regexp.MustCompile(`(?i)a[^b]*b`)
)

// stripInlineScriptsPreFix is uihost's head scrub before 36abe258: each
// removal runs once, so <scr<script></script>ipt> leaves <script>.
func stripInlineScriptsPreFix(s string) string {
	out := scriptTagRe.ReplaceAllString(s, "")      // want `sanitizefix: stripInlineScriptsPreFix removes a multi-character match in one pass`
	out = autofocusAttrRe.ReplaceAllString(out, "") // want `sanitizefix: stripInlineScriptsPreFix removes a multi-character match in one pass`
	return out
}

// stripInlineScripts is the fix: the single pass runs to a fixpoint,
// failing closed at the cap.
func stripInlineScripts(s string) string {
	for range 8 {
		next := stripInlineScriptsOnce(s)
		if next == s {
			return s
		}
		s = next
	}
	return ""
}

// stripInlineScriptsOnce is quiet: its only caller loops it.
func stripInlineScriptsOnce(s string) string {
	out := scriptTagRe.ReplaceAllString(s, "")
	return autofocusAttrRe.ReplaceAllString(out, "")
}

// cleanPath is the textbook case: "....//" becomes "../".
func cleanPath(p string) string {
	return strings.ReplaceAll(p, "../", "") // want `sanitizefix: cleanPath removes a multi-character match in one pass`
}

// cleanPathLoop is the loop spelling: remove while any remains.
func cleanPathLoop(p string) string {
	for strings.Contains(p, "../") {
		p = strings.ReplaceAll(p, "../", "")
	}
	return p
}

func sanitizeBytes(b []byte) []byte {
	return bytes.Replace(b, []byte("<!--"), nil, -1) // want `sanitizefix: sanitizeBytes removes a multi-character match in one pass`
}

func stripANSI(s string) string {
	return ansiRe.ReplaceAllString(s, "") // want `sanitizefix: stripANSI removes a multi-character match in one pass`
}

// stripNarrowTags: <a<b>> loses <b> and leaves <a>.
func stripNarrowTags(s string) string {
	return narrowTagRe.ReplaceAllString(s, "") // want `sanitizefix: stripNarrowTags removes a multi-character match in one pass`
}

// A case-folded delimited pattern is not credited: folding widens the
// delimiters past the one-rune proof.
func stripFolded(s string) string {
	return foldedDelimitedRe.ReplaceAllString(s, "") // want `sanitizefix: stripFolded removes a multi-character match in one pass`
}

// --- quiet ---------------------------------------------------------------

// Single characters, and runs of one class, cannot recombine.
func scrubControls(s string) string {
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\x00", "")
	return ctlRe.ReplaceAllString(s, "")
}

// <[^>]+> is the provably idempotent delimited form.
func stripTags(s string) string {
	return anyTagRe.ReplaceAllString(s, "")
}

// A replacement that leaves a separator keeps the neighbours apart.
func scrubToSpace(s string) string {
	return scriptTagRe.ReplaceAllString(s, " ")
}

// A normaliser whose name says nothing about cleaning is not a scrub.
func normalizeSQL(s string) string {
	return unquoteIdentRe.ReplaceAllString(s, "")
}

// A locally compiled pattern is as good as a package-level one: a
// single-character removal through it cannot recombine.
func scrubLocalSingle(s string) string {
	re := regexp.MustCompile("\r")
	return re.ReplaceAllString(s, "")
}

// A local multi-character pattern still reports.
func stripLocalMulti(s string) string {
	re := regexp.MustCompile("ab")
	return re.ReplaceAllString(s, "") // want `sanitizefix: stripLocalMulti removes a multi-character match in one pass`
}
