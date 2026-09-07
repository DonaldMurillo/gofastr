//go:build red

package runtime

import (
	"regexp"
	"strings"
	"testing"
)

// RED TEST — open finding, 2026-09-06 adversarial pass (round 5; tests-only; no fix applied).
// Property: malformed data-fui-* attribute values degrade to a no-op — never throw out of
// delegated handlers. The family is pinned for selectors (TestSelectorByDesignLookupsGuarded);
// the data-fui-deeplink decode sites are unguarded.
// Surfaces: runtime.js::_installEagerWidgetDelegators [data-fui-open] click handler (composed
// byte-identical with its fragment twins frag/widgets-boot.js and frag/widgets-boot-static.js —
// all three asserted so the pin holds at whichever a fix lands on first);
// src/lightbox.js::parseDeeplink and ::srcOf (called from step() click/keydown handlers and
// recordOpen's MutationObserver).
// Finding: [deeplink-decode-throws] the click handler decodes data-fui-deeplink pairs with
// decodeURIComponent under no try, AFTER preventDefault() — decodeURIComponent('%E0%A4')
// throws URIError, so the widget open is consumed and nothing opens. [lightbox-decode-throws]
// src/lightbox.js decodes the same attribute from parseDeeplink and srcOf unguarded — one
// malformed escape kills gallery nav.
// Fix direction: wrap every data-fui-deeplink decode in try/catch (or route it through a
// same-file safeDecode helper whose body wraps decodeURIComponent in try), mirroring the
// selector guard family.

// redDecodeFindings checks one extracted window: every decodeURIComponent(
// in it must sit inside a try block (insideTryBlock), or the window must
// route through a same-file helper (a non-dotted FN(...) call) whose
// declared body wraps decodeURIComponent in a try. Returns one entry per
// unguarded decode; a window with no decode and no guarded helper route is
// reported as setup drift (the pinned surface moved).
func redDecodeFindings(t *testing.T, fileSrc, window, where string) []string {
	t.Helper()
	const dec = "decodeURIComponent("
	if strings.Count(window, dec) == 0 {
		if !redWindowRoutesGuardedHelper(fileSrc, window) {
			t.Fatalf("setup broken: %s no longer decodes data-fui-deeplink and calls no same-file decode helper — surface drifted", where)
		}
		return nil
	}
	var bad []string
	off := 0
	for {
		i := strings.Index(window[off:], dec)
		if i < 0 {
			break
		}
		pos := off + i
		if !insideTryBlock(window, pos) {
			bad = append(bad, where)
		}
		off = pos + len(dec)
	}
	return bad
}

// redCall matches a non-dotted identifier call FN( — dotted method calls
// (.getAttribute() etc.) are excluded because only same-file function
// declarations count as the helper route.
var redCall = regexp.MustCompile(`(?:^|[^\w$.])([A-Za-z_$][A-Za-z0-9_$]*)\s*\(`)

// redWindowRoutesGuardedHelper reports whether window calls any same-file
// function (function FN(…){…} or const/let/var FN = (…)=>{…}) whose body
// wraps decodeURIComponent in a try block.
func redWindowRoutesGuardedHelper(fileSrc, window string) bool {
	for _, m := range redCall.FindAllStringSubmatch(window, -1) {
		ident := m[1]
		if ident == "decodeURIComponent" {
			continue
		}
		declRe := regexp.MustCompile(
			`(?:function\s+` + regexp.QuoteMeta(ident) + `\s*\(` +
				`|(?:const|let|var)\s+` + regexp.QuoteMeta(ident) + `\s*=)`)
		loc := declRe.FindStringIndex(fileSrc)
		if loc == nil {
			continue
		}
		rel := strings.Index(fileSrc[loc[1]:], "{")
		if rel < 0 || rel > 200 { // decl far from any body: not a function
			continue
		}
		b0 := loc[1] + rel
		b1 := funcBodyEnd(fileSrc, b0)
		if b1 <= b0 {
			continue
		}
		body := fileSrc[b0:b1]
		if k := strings.Index(body, "decodeURIComponent("); k >= 0 && insideTryBlock(body, k) {
			return true
		}
	}
	return false
}

// TestDeeplinkRedDecodeThrows pins the eager widget-open delegator's
// deeplink parse: every decodeURIComponent on data-fui-deeplink pairs runs
// inside a try (or a guarded same-file helper), because the decode happens
// AFTER preventDefault() — a malformed escape (%E0%A4) throws URIError, the
// default navigation is already suppressed, and nothing opens. Asserted on
// the composed runtime.js and both fragment twins (widgets-boot,
// widgets-boot-static) so the pin holds wherever a fix lands. Vacuity
// controls: the pre-fix bare spelling must keep firing, the try-wrapped
// spelling must not.
func TestDeeplinkRedDecodeThrows(t *testing.T) {
	for _, rel := range []string{
		"runtime.js",
		"frag/widgets-boot.js",
		"frag/widgets-boot-static.js",
	} {
		src := readSrc(t, rel)
		start := strings.Index(src, "const raw = btn.getAttribute('data-fui-deeplink') || '';")
		if start < 0 {
			t.Fatalf("setup broken: could not locate the data-fui-deeplink read in %s", rel)
		}
		endRel := strings.Index(src[start:], "const anchorPref")
		if endRel < 0 {
			t.Fatalf("setup broken: could not locate 'const anchorPref' after the deeplink read in %s", rel)
		}
		window := src[start : start+endRel]
		for range redDecodeFindings(t, src, window, rel+" [data-fui-open] handler") {
			t.Errorf("SECURITY: [deeplink-decode-throws] %s: data-fui-deeplink pairs decoded with bare decodeURIComponent after preventDefault() — decodeURIComponent('%%E0%%A4') throws URIError, the open click is already consumed, and the widget never opens; the degrade-don't-throw family pinned for selectors (TestSelectorByDesignLookupsGuarded) must cover decode sites too", rel)
		}
	}

	// Vacuity control: the check must keep firing on the pre-fix spelling.
	preSrc := "function onClick(btn) {\n" +
		"  const raw = btn.getAttribute('data-fui-deeplink') || '';\n" +
		"  const overrides = {};\n" +
		"  if (raw) {\n" +
		"    for (const pair of raw.split('&')) {\n" +
		"      const eq = pair.indexOf('=');\n" +
		"      if (eq < 0) continue;\n" +
		"      overrides[decodeURIComponent(pair.slice(0, eq))] =\n" +
		"        decodeURIComponent(pair.slice(eq + 1));\n" +
		"    }\n" +
		"  }\n" +
		"}\n"
	preWin := preSrc[strings.Index(preSrc, "const raw"):strings.LastIndex(preSrc, "  }\n")]
	if len(redDecodeFindings(t, preSrc, preWin, "vacuity control")) == 0 {
		t.Error("VACUITY: decode guard check no longer fires on the pre-fix bare decodeURIComponent spelling")
	}
	// Negative control: the fixed try-wrapped spelling must stay green.
	fixSrc := "function onClick(btn) {\n" +
		"  const overrides = {};\n" +
		"  try {\n" +
		"    overrides[decodeURIComponent('a')] = decodeURIComponent('b');\n" +
		"  } catch (_) {}\n" +
		"}\n"
	fixWin := fixSrc[strings.Index(fixSrc, "try {"):strings.Index(fixSrc, "catch")]
	if len(redDecodeFindings(t, fixSrc, fixWin, "negative control")) != 0 {
		t.Error("VACUITY: decode guard check fires on the try-wrapped (fixed) spelling")
	}
}

// TestLightboxRedDecodeThrows pins the lightbox module's deeplink decode:
// parseDeeplink and srcOf both split data-fui-deeplink and decode each pair
// bare; parseDeeplink runs from step() (gallery prev/next click and
// keydown) and recordOpen (MutationObserver), srcOf from the lightbox
// chrome. A malformed escape throws out of those handlers and kills gallery
// nav instead of degrading to a no-op (empty src / skipped pair).
func TestLightboxRedDecodeThrows(t *testing.T) {
	src := readSrc(t, "src/lightbox.js")
	windows := [][2]string{
		{"function parseDeeplink(s)", "function step"},
		{"function srcOf(anchor)", "function parseDeeplink"},
	}
	for _, w := range windows {
		start := strings.Index(src, w[0])
		if start < 0 {
			t.Fatalf("setup broken: could not locate %q in src/lightbox.js", w[0])
		}
		endRel := strings.Index(src[start:], w[1])
		if endRel < 0 {
			t.Fatalf("setup broken: could not locate %q after %q in src/lightbox.js", w[1], w[0])
		}
		window := src[start : start+endRel]
		for range redDecodeFindings(t, src, window, "src/lightbox.js "+w[0]) {
			t.Errorf("SECURITY: [lightbox-decode-throws] src/lightbox.js %s decodes data-fui-deeplink pairs with bare decodeURIComponent — a malformed escape (decodeURIComponent('%%E0%%A4') throws URIError) throws out of step()'s click/keydown handlers and recordOpen's MutationObserver, killing gallery nav instead of degrading to a no-op", w[0])
		}
	}
}
