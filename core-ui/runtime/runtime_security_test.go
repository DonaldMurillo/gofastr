package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The runtime is shipped as JavaScript with no JS engine available in
// the Go test process. These tests assert security properties on the
// JS *source* (the same surface the minify tests pin), since that is
// the artifact that ships to browsers.

func readSrc(t *testing.T, rel string) string {
	t.Helper()
	// Tests run from the package dir (core-ui/runtime). runtime.js sits
	// alongside; src/* under src/.
	b, err := os.ReadFile(rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

// schemeGuardStripsInteriorControls asserts the URL-scheme guard
// (_isUnsafeSignalUrl) neutralises javascript:/vbscript: even when the
// scheme carries embedded tab/newline/CR or leading C0 control bytes —
// the chars browsers strip during URL parsing before scheme detection.
//
// Surface: the strip step in _isUnsafeSignalUrl, which is the single
// choke point for setSignal attr-mode (href/src/action) and navigate().
func TestSchemeGuardStripsInteriorControls(t *testing.T) {
	src := readSrc(t, "runtime.js")

	// Locate the normalization step inside _isUnsafeSignalUrl.
	fnIdx := strings.Index(src, "_isUnsafeSignalUrl(attr, value)")
	if fnIdx < 0 {
		t.Fatal("could not locate _isUnsafeSignalUrl in runtime.js")
	}
	body := src[fnIdx:]
	end := strings.Index(body, "register(id")
	if end > 0 {
		body = body[:end]
	}

	// The old guard anchored the strip with `^`, so only the LEADING run
	// of control chars was removed — an interior tab/newline left the
	// scheme intact and startsWith() returned false. The fixed guard must
	// NOT anchor with `^`. (The class itself uses literal C0 bytes for
	// the 0x00-0x1f range, so we match the regexp literal generically.)
	// The char class spans `\s` plus the literal C0 range bytes
	// (0x00, '-' as a range operator, 0x1f). Capture the anchor and flag.
	stripRe := regexp.MustCompile("(?s)replace\\(/(\\^?)\\[\\\\s[\x00-\x2f]+\\]\\+/(g?),")
	m := stripRe.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("SECURITY: [scheme-guard] could not locate the control-char strip regex in _isUnsafeSignalUrl; body:\n%s", body)
	}
	if m[1] == "^" {
		t.Error("SECURITY: [scheme-guard] _isUnsafeSignalUrl anchors the strip with `^` — only the leading control-char run is removed, so interior tab/newline (java\\tscript:) defeats startsWith()")
	}
	if m[2] != "g" {
		t.Error("SECURITY: [scheme-guard] _isUnsafeSignalUrl strip is not global (`/g`) — interior control chars survive and the scheme check is bypassable")
	}
	// The class must still span the full C0 control range (0x00-0x1f);
	// verify a literal NUL and a literal 0x1f both appear inside it.
	classStart := strings.Index(body, `replace(/`)
	classEnd := strings.Index(body[classStart:], `]+/`)
	class := body[classStart : classStart+classEnd]
	if !strings.ContainsRune(class, 0x00) || !strings.ContainsRune(class, 0x1f) {
		t.Error("SECURITY: [scheme-guard] strip char class no longer covers the full C0 range (0x00-0x1f)")
	}
}

// csrfHeaderForwardedOnRPC asserts every state-changing fetch from the
// runtime forwards the CSRF token (X-CSRF-Token from the
// meta[name="csrf-token"] tag) — the documented channel the auth.CSRF
// middleware accepts for JSON-bodied requests.
//
// Surfaces: core dispatchRPC (runtime.js), widget-scoped dispatchRPC
// (src/widgets.js), infinite-scroll fetch (src/infinitescroll.js).
func TestCsrfHeaderForwardedOnRPC(t *testing.T) {
	surfaces := []string{
		"runtime.js",
		filepath.Join("src", "widgets.js"),
		filepath.Join("src", "infinitescroll.js"),
	}
	for _, rel := range surfaces {
		src := readSrc(t, rel)
		// The reference fix (toggleaction.js / optimisticaction.js) reads
		// meta[name="csrf-token"] and sets X-CSRF-Token. Every RPC surface
		// must do the same so the documented CSRF middleware accepts the
		// request.
		if !strings.Contains(src, `meta[name="csrf-token"]`) {
			t.Errorf("SECURITY: [csrf] %s never reads meta[name=\"csrf-token\"] — state-changing fetch is missing the CSRF token", rel)
		}
		if !strings.Contains(src, "X-CSRF-Token") {
			t.Errorf("SECURITY: [csrf] %s never sets the X-CSRF-Token header — auth.CSRF middleware rejects the JSON RPC", rel)
		}
	}
}

// htmlSignalDoesNotInjectObjectMarkup asserts html-mode signal rendering
// never routes a non-string value (e.g. the auto-built dispatchRPC error
// object {ok:false,status,text}) through innerHTML. JSON.stringify does
// NOT HTML-escape, so a server error body that reflects attacker input
// ("<img src=x onerror=…>") would execute. Non-string values must use
// textContent (mirroring text-mode); the html escape hatch is for
// trusted HTML *strings* only.
//
// Surface: the html-mode branch of setSignal in runtime.js.
func TestHtmlSignalDoesNotInjectObjectMarkup(t *testing.T) {
	src := readSrc(t, "runtime.js")
	fnIdx := strings.Index(src, "setSignal(name, value)")
	if fnIdx < 0 {
		t.Fatal("could not locate setSignal in runtime.js")
	}
	body := src[fnIdx:]
	if end := strings.Index(body, "signal(name) {"); end > 0 {
		body = body[:end]
	}
	htmlIdx := strings.Index(body, "if (mode === 'html')")
	if htmlIdx < 0 {
		t.Fatal("could not locate html-mode branch in setSignal")
	}
	// Capture just the html-mode branch up to the next mode check.
	htmlBranch := body[htmlIdx:]
	if end := strings.Index(htmlBranch, "} else if (mode === 'attr')"); end > 0 {
		htmlBranch = htmlBranch[:end]
	}

	// The vulnerable shape assigns JSON.stringify(value) into innerHTML
	// for the non-string case. The fixed shape must NOT feed
	// JSON.stringify output to innerHTML — non-string values go to
	// textContent. Detect the unsafe pairing: innerHTML on the same
	// statement as JSON.stringify.
	for _, line := range strings.Split(htmlBranch, "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "innerHTML") && strings.Contains(l, "JSON.stringify") {
			t.Errorf("SECURITY: [html-signal] non-string signal value reaches innerHTML via JSON.stringify (no HTML-escape) — a reflected RPC error object {text:'<img onerror=…>'} executes; line:\n%s", l)
		}
	}
}

// sseIslandSelectorEscaped asserts the SSE island handler escapes the
// server-supplied island name before interpolating it into a CSS
// attribute selector. Without CSS.escape() a crafted island name like
// `x"], [data-trusted-region` re-targets the write to an unintended
// element (and `x"]` throws an invalid-selector error that silently
// drops the legitimate island's update).
//
// Surface: the island event listener in src/sse.js. Sibling widgets.js /
// toasts.js already wrap analogous data-* lookups in CSS.escape().
func TestSseIslandSelectorEscaped(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "sse.js"))

	if !strings.Contains(src, "CSS.escape") {
		t.Error("SECURITY: [sse-selector] src/sse.js never calls CSS.escape on the island name — the SSE island field is interpolated raw into a CSS attribute selector, so a crafted name re-targets the innerHTML write")
	}

	// The raw-template form `[data-island="${island}"]` is the vulnerable
	// shape; once fixed the island name must be escaped, not templated
	// directly into the selector.
	if strings.Contains(src, "[data-island=\"${island}\"]") {
		t.Error("SECURITY: [sse-selector] src/sse.js still interpolates the raw island name into the selector template `[data-island=\"${island}\"]` — must use CSS.escape(String(island))")
	}
}
