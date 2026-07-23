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
	// NOT anchor with `^`. The char class spans `\s` plus the C0 control
	// range; it is written with `\x00-\x1f` escapes (raw control bytes in
	// source make the file "binary" — see TestRuntimeJSIsCleanText). We
	// match the class interior generically and validate range coverage
	// below, so both the escaped and (legacy) raw-byte forms are accepted.
	// Capture the anchor and the global flag.
	stripRe := regexp.MustCompile("(?s)replace\\(/(\\^?)\\[\\\\s[^\\]]+\\]\\+/(g?),")
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
	// The class must still span the full C0 control range (0x00-0x1f).
	// Accept either the escaped form (`\x00-\x1f`) or the legacy raw
	// control bytes — both denote the identical JS character-class range.
	classStart := strings.Index(body, `replace(/`)
	classEnd := strings.Index(body[classStart:], `]+/`)
	class := body[classStart : classStart+classEnd]
	escapedRange := strings.Contains(class, `\x00`) && strings.Contains(class, `\x1f`)
	rawRange := strings.ContainsRune(class, 0x00) && strings.ContainsRune(class, 0x1f)
	if !escapedRange && !rawRange {
		t.Error("SECURITY: [scheme-guard] strip char class no longer covers the full C0 range (0x00-0x1f)")
	}
}

// csrfHeaderForwardedOnRPC asserts every state-changing fetch from the
// runtime forwards the CSRF token (X-CSRF-Token from the
// meta[name="csrf-token"] tag) — the documented channel the auth.CSRF
// middleware accepts for JSON-bodied requests.
//
// Surfaces: core dispatchRPC (runtime.js), widget-scoped dispatchRPC
// (src/widgets.js), infinite-scroll fetch (src/infinitescroll.js),
// sortablelist reorder POST (src/sortablelist.js), and the kiln-tool
// POST (runtime.js) — every state-changing fetch() in the runtime.
func TestCsrfHeaderForwardedOnRPC(t *testing.T) {
	surfaces := []string{
		"runtime.js",
		filepath.Join("src", "widgets.js"),
		filepath.Join("src", "infinitescroll.js"),
		filepath.Join("src", "sortablelist.js"),
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

	// The kiln-tool POST is a distinct fetch site from dispatchRPC. Pin the
	// call-site window so a file-level CSRF match elsewhere cannot mask it.
	w := readSrc(t, "runtime.js")
	tIdx := strings.Index(w, "fetch('/kiln/tool/")
	if tIdx < 0 {
		t.Fatal("could not locate kiln-tool POST in runtime.js")
	}
	// The headers block sits just before/around the URL in the kiln-tool
	// handler; scan a window spanning both sides of the call site.
	start := tIdx - 400
	if start < 0 {
		start = 0
	}
	end := tIdx + 400
	if end > len(w) {
		end = len(w)
	}
	if !strings.Contains(w[start:end], "_csrf") {
		t.Error("SECURITY: [csrf] runtime.js kiln-tool POST (/kiln/tool/) does not use _csrf — auth.CSRF middleware rejects the JSON RPC")
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

// htmlSignalSkipsNonStringAsserts pins the stronger invariant beyond
// TestHtmlSignalDoesNotInjectObjectMarkup: in html mode, a non-string
// value (the dispatchRPC error object {ok:false,status,text} broadcast
// on every non-2xx) must NOT touch the DOM at all. The earlier textContent
// fallback was XSS-safe but still overwrote the trusted region with a
// JSON blob, corrupting the list on every failed optimistic delete/create.
// The optimistic-UI cookbook relies on this no-op: "a failed delete leaves
// the row/list unchanged."
//
// Surface: the html-mode branch of setSignal in runtime.js.
func TestHtmlSignalSkipsNonStringValues(t *testing.T) {
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
	htmlBranch := body[htmlIdx:]
	if end := strings.Index(htmlBranch, "} else if (mode === 'attr')"); end > 0 {
		htmlBranch = htmlBranch[:end]
	}
	// Must guard non-string values with an early return so a broadcast
	// error object never reaches innerHTML OR textContent.
	if !strings.Contains(htmlBranch, "typeof value !== 'string'") &&
		!strings.Contains(htmlBranch, "typeof value != 'string'") {
		t.Error("SECURITY: [html-signal] html-mode branch must early-return on non-string values (typeof value !== 'string') so a failed-RPC error object does not overwrite the trusted region")
	}
	// The corruption shape — writing JSON.stringify(value) into textContent
	// — must be gone from the html-mode branch entirely.
	for _, line := range strings.Split(htmlBranch, "\n") {
		l := strings.TrimSpace(line)
		if strings.Contains(l, "textContent") && strings.Contains(l, "JSON.stringify") {
			t.Errorf("SECURITY: [html-signal] html-mode branch still writes JSON.stringify(value) into textContent — a non-2xx broadcast overwrites the trusted region with a JSON blob; line:\n%s", l)
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

// seedLoopsSkipReservedKeys asserts both signal-seed merge loops in
// runtime.js skip the JS reserved object keys (__proto__, constructor,
// prototype) before assigning `store[k] = …`. With a string key of
// "__proto__", the bracket assignment `store["__proto__"] = {…}` invokes
// the __proto__ setter and re-parents the _signals store object — a
// crafted seed then re-routes every not-yet-set signal name through the
// attacker's object (cross-signal confusion) and makes setSignal mutate
// the shared prototype instead of an own property. The host-generated
// seed keys are server-controlled today, but skipping reserved keys is
// cheap, advisory-recommended (strip __proto__/constructor/prototype
// before merging) hardening.
//
// Surfaces: the boot seed loop (window.__gofastr_signals_seed) and BOTH
// the page (data.p) and global (data.g) loops in mergeSeedFromDOM.
func TestSeedLoopsSkipReservedKeys(t *testing.T) {
	src := readSrc(t, "runtime.js")

	// Locate each of the three merge loops by an anchor unique to it and
	// require a reserved-key skip guard inside the loop body.
	type loop struct {
		name   string
		anchor string // substring marking the start of the loop body
		end    string // substring marking the end of the loop body
	}
	loops := []loop{
		{"boot-seed", "const seed = window.__gofastr_signals_seed;", "// -----"},
		{"merge-page", "const page = data.p || {};", "const glob = data.g || {};"},
		{"merge-global", "const glob = data.g || {};", "  };\n\n  const swapMainContent"},
	}
	for _, lp := range loops {
		i := strings.Index(src, lp.anchor)
		if i < 0 {
			t.Fatalf("could not locate %s loop anchor %q in runtime.js", lp.name, lp.anchor)
		}
		body := src[i:]
		if j := strings.Index(body, lp.end); j > 0 {
			body = body[:j]
		}
		// The fix must reject the three reserved keys before the
		// store[k] = … assignment. Accept either a helper call
		// (isReservedSignalKey) or an inline check naming all three.
		hasHelper := strings.Contains(body, "isReservedSignalKey(")
		hasInline := strings.Contains(body, "__proto__") &&
			strings.Contains(body, "constructor") &&
			strings.Contains(body, "prototype")
		if !hasHelper && !hasInline {
			t.Errorf("SECURITY: [proto-pollution] %s loop does not skip reserved keys (__proto__/constructor/prototype) before store[k] assignment — a seed key of \"__proto__\" re-parents the _signals store. Body:\n%s", lp.name, body)
		}
	}
}

// computedReducerOwnPropOnly asserts the computed module looks the
// reducer up as an OWN property of _reducers, not via the prototype
// chain. The `typeof fn === 'function'` guard alone does NOT protect
// against inherited Object.prototype methods: when no reducer named
// "constructor" / "toString" / "valueOf" is registered,
// `G._reducers["constructor"]` resolves to Object (typeof === 'function')
// and gets invoked as a reducer, breaking the documented "missing
// reducer → no-op" contract. The fix gates the lookup on
// Object.prototype.hasOwnProperty.call(_reducers, name).
//
// Surface: the recompute() reducer lookup in src/computed.js.
func TestComputedReducerOwnPropOnly(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "computed.js"))

	fnIdx := strings.Index(src, "const recompute = ")
	if fnIdx < 0 {
		t.Fatal("could not locate recompute in src/computed.js")
	}
	body := src[fnIdx:]
	if end := strings.Index(body, "// Subscribe to every dependency"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "hasOwnProperty") {
		t.Error("SECURITY: [computed-reducer] recompute resolves the reducer without an own-property guard — `_reducers[\"constructor\"]` resolves to the inherited Object (typeof 'function') and gets invoked, bypassing the missing-reducer no-op contract. Gate on Object.prototype.hasOwnProperty.call(_reducers, reducerName).")
	}
}
