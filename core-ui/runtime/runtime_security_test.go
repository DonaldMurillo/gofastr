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
