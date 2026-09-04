package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
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
// scheme carries embedded tab/newline/CR or leading C0 control bytes,
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
	// of control chars was removed, an interior tab/newline left the
	// scheme intact and startsWith() returned false. The fixed guard must
	// NOT anchor with `^`. The char class spans `\s` plus the C0 control
	// range; it is written with `\x00-\x1f` escapes (raw control bytes in
	// source make the file "binary". See TestRuntimeJSIsCleanText). We
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
	// control bytes, both denote the identical JS character-class range.
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
// meta[name="csrf-token"] tag), the documented channel the auth.CSRF
// middleware accepts for JSON-bodied requests.
//
// Surfaces: shared dispatchRPC + kiln POST (src/rpc.js), infinite-scroll
// (src/infinitescroll.js), and sortable-list reorder
// (src/sortablelist.js).
func TestCsrfHeaderForwardedOnRPC(t *testing.T) {
	surfaces := []string{
		filepath.Join("src", "rpc.js"),
		filepath.Join("src", "infinitescroll.js"),
		filepath.Join("src", "sortablelist.js"),
	}
	for _, rel := range surfaces {
		src := readSrc(t, rel)
		// A surface may read meta[name="csrf-token"] directly or call the
		// shared _csrf helper in its own module.
		if !strings.Contains(src, `meta[name="csrf-token"]`) && !strings.Contains(src, "_csrf(") {
			t.Errorf("SECURITY: [csrf] %s neither reads meta[name=\"csrf-token\"] nor calls _csrf — state-changing fetch is missing the CSRF token", rel)
		}
		if !strings.Contains(src, "X-CSRF-Token") && !strings.Contains(src, "_csrf(") {
			t.Errorf("SECURITY: [csrf] %s never sets the X-CSRF-Token header — auth.CSRF middleware rejects the JSON RPC", rel)
		}
	}

	// The kiln-tool POST is a distinct fetch site from dispatchRPC. Pin the
	// call-site window so a file-level CSRF match elsewhere cannot mask it.
	w := readSrc(t, filepath.Join("src", "rpc.js"))
	tIdx := strings.Index(w, "fetch('/kiln/tool/")
	if tIdx < 0 {
		t.Fatal("could not locate kiln-tool POST in src/rpc.js")
	}
	// The headers block sits just before/around the URL in the kiln-tool
	// handler; scan a window spanning both sides of the call site.
	start := max(tIdx-400, 0)
	end := min(tIdx+400, len(w))
	if !strings.Contains(w[start:end], "_csrf") {
		t.Error("SECURITY: [csrf] src/rpc.js kiln-tool POST (/kiln/tool/) does not use _csrf — auth.CSRF middleware rejects the JSON RPC")
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
	fnIdx := strings.Index(src, "setSignal(name, value, opts)")
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
	// JSON.stringify output to innerHTML, non-string values go to
	// textContent. Detect the unsafe pairing: innerHTML on the same
	// statement as JSON.stringify.
	for line := range strings.SplitSeq(htmlBranch, "\n") {
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
	fnIdx := strings.Index(src, "setSignal(name, value, opts)")
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
	// The corruption shape, writing JSON.stringify(value) into textContent,
	// must be gone from the html-mode branch entirely.
	for line := range strings.SplitSeq(htmlBranch, "\n") {
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
// the __proto__ setter and re-parents the _signals store object, a
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

// comboboxFallbackChecksScheme pins the defense-in-depth contract for
// pickOption's hard-load fallback: when window.__gofastr.navigate has
// not booted, the raw `location.href = dest` assignment must still be
// dominated by the same scheme/origin gate the SPA navigator uses
// (_originOK subsumes _isUnsafeSignalUrl's javascript:/vbscript:/
// non-image data: checks — see TestRuntimeNavigateRejectsUnsafeSchemes
// in runtime_test.go). dest is a server-rendered data-fui-push-state
// attribute on RPC-injected option markup; a server that reflects
// request input into it must not gain a scheme bypass just because the
// runtime booted late.
//
// The sibling TestComboboxPickOptionHonorsPushState only requires
// "navigate" to appear anywhere in the module, so weakening the
// fallback would not fail it — this test is the pin that would.
//
// Dominance is approximated lexically, per enclosing block: a gate
// counts when it appears in an enclosing block's own if/else-if
// condition or in the statements preceding the assignment inside its
// own block. An if-statement a bare `else` attaches to does not
// dominate the else branch, so its condition is excluded.
//
// Surface: the fallback branch of pickOption in src/combobox.js.
func TestComboboxFallbackChecksScheme(t *testing.T) {
	src := readSrc(t, filepath.Join("src", "combobox.js"))

	idx := strings.Index(src, "pickOption")
	if idx < 0 {
		t.Fatal("could not locate pickOption in src/combobox.js")
	}
	open := idx + strings.Index(src[idx:], "{")
	if open < idx {
		t.Fatal("could not locate pickOption body opener")
	}
	end := -1
	for depth, i := 0, open; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		t.Fatal("could not locate pickOption body end")
	}
	body := src[open+1 : end]

	foundAssignment := false
	for start := 0; ; {
		rel := strings.Index(body[start:], "location.href")
		if rel < 0 {
			break
		}
		pos := start + rel
		start = pos + len("location.href")

		// Only assignments (`location.href = …`), not reads or
		// strict-equality comparisons.
		tail := strings.TrimLeft(body[start:], " \t\r\n")
		if !strings.HasPrefix(tail, "=") || strings.HasPrefix(tail, "==") {
			continue
		}
		foundAssignment = true

		// Walk the chain of blocks enclosing the assignment,
		// innermost first. A gate satisfies the property when it
		// appears in a block's own condition or in the statements of
		// that block preceding the nested statement the chain
		// descends into.
		gated := false
		reportStart := pos
		childStmt := pos
		for p := pos; ; {
			opener := innermostOpenerBefore(body, p)
			if opener < 0 {
				break
			}
			cond, nodeStart := "", opener
			if j := skipSpaceBack(body, opener-1); j >= 0 && body[j] == ')' {
				if k := matchParenBack(body, j); k >= 0 {
					cond = body[k : j+1]
					nodeStart = k
				}
			}
			// Cut the node's statement window at the start of the
			// statement the chain descends into. A bare `else` block
			// is part of the preceding if-statement, whose condition
			// does NOT dominate the else branch.
			cut := childStmt
			if cut != pos {
				j := skipSpaceBack(body, cut-1)
				if (j >= 0 && body[j] == '}') || isBareElseOpen(body, cut) {
					cut = ifStmtStartBefore(body, cut)
				}
			}
			if opener+1 <= cut && cut <= len(body) {
				window := cond + body[opener+1:cut]
				if strings.Contains(window, "_isUnsafeSignalUrl") || strings.Contains(window, "_originOK") {
					gated = true
				}
			}
			if nodeStart < reportStart {
				reportStart = nodeStart
			}
			if nodeStart == 0 {
				break
			}
			p = nodeStart
			childStmt = nodeStart
		}
		if !gated {
			t.Errorf("SECURITY: [combobox-nav] pickOption assigns location.href without a dominating _isUnsafeSignalUrl/_originOK gate; guard window:\n%s", body[reportStart:pos+len("location.href")])
		}
	}
	if !foundAssignment {
		t.Error("pickOption no longer contains a location.href fallback — update this pin to the new navigation form")
	}
}

// innermostOpenerBefore returns the index of the innermost unmatched `{`
// at or before pos-1 in s, or -1. Scanning backwards, a `}` raises the
// depth and the `{` that pairs with it is skipped.
func innermostOpenerBefore(s string, pos int) int {
	depth := 0
	for i := pos - 1; i >= 0; i-- {
		switch s[i] {
		case '}':
			depth++
		case '{':
			if depth == 0 {
				return i
			}
			depth--
		}
	}
	return -1
}

// matchParenBack returns the index of the `(` that pairs with the `)` at
// index i in s, or -1.
func matchParenBack(s string, i int) int {
	depth := 0
	for ; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			if depth <= 1 {
				return i
			}
			depth--
		}
	}
	return -1
}

// skipSpaceBack returns the index of the last non-space char at or
// before pos in s, or -1.
func skipSpaceBack(s string, pos int) int {
	for i := pos; i >= 0; i-- {
		switch s[i] {
		case ' ', '\t', '\r', '\n':
		default:
			return i
		}
	}
	return -1
}

// isBareElseOpen reports whether the block opener at index i in s is a
// bare `else {` (no condition of its own), i.e. the preceding token is
// the else keyword.
func isBareElseOpen(s string, i int) bool {
	j := skipSpaceBack(s, i-1)
	if j < 3 || j-3 > len(s)-1 {
		return false
	}
	if s[j-3:j+1] != "else" {
		return false
	}
	// Reject identifiers merely ending in "else" (e.g. `xelse`).
	if k := j - 4; k >= 0 {
		c := s[k]
		if c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return false
		}
	}
	return true
}

// ifStmtStartBefore returns the start of the if-statement the bare
// `else` block at index elseOpen belongs to: its condition's `(` when
// the associated if-block is headed by one, else that block's `{`.
func ifStmtStartBefore(s string, elseOpen int) int {
	j := skipSpaceBack(s, elseOpen-1)
	if j >= 3 && s[j-3:j+1] == "else" {
		// Bare `else {` — hop over the keyword to the if-block's `}`.
		j = skipSpaceBack(s, j-4)
	}
	if j < 0 || s[j] != '}' {
		return elseOpen
	}
	depth := 0
	open := -1
	for i := j; i >= 0; i-- {
		switch s[i] {
		case '}':
			depth++
		case '{':
			depth--
			if depth == 0 {
				open = i
			}
		}
		if open >= 0 {
			break
		}
	}
	if open < 0 {
		return elseOpen
	}
	if k := skipSpaceBack(s, open-1); k >= 0 && s[k] == ')' {
		if m := matchParenBack(s, k); m >= 0 {
			return m
		}
	}
	return open
}

// moduleSrcValidatesNameShape asserts the split-module loader validates
// its name against the shape the framework emits before building a
// script src from it. loadModule's name arrives from the DOM: the
// data-fui-prefetch hover/focus path (_prefetch) splits the attribute
// on whitespace and passes each token straight through, so a token like
// ../../../evil interpolates into '/__gofastr/runtime/' + name + '.js'
// and the browser normalizes the request to /evil.js — past the
// runtime-module serve route (TrimPrefix map-miss) and onto whatever
// same-origin JS route the host serves, the exact threat the
// data-behavior gate's comment documents and the reason
// TestBehaviorAttrRejectsForeignSrc pins that sibling. Every emitted
// name (runtime.ModuleNames, and every internal loadModule call site:
// rpc, widgets, toasts, popover, fileupload) is [A-Za-z0-9_-]+, so an
// anchored allow-list rejects nothing legitimate.
//
// Surfaces: loadModule in the composed runtime.js AND its fragment
// source frag/boot.js (the composition gate keeps them byte-identical;
// both are asserted so the pin holds at whichever a fix lands on
// first). Dominance is approximated lexically: the guard counts when
// it appears between function entry and the URL build, the same
// approximation the combobox scheme-gate pin uses.
//
// Behavioural proof: TestPrefetchAttrRejectsForeignModule
// (gadget_e2e_test.go).
func TestModuleSrcValidatesNameShape(t *testing.T) {
	for _, rel := range []string{"runtime.js", "frag/boot.js"} {
		src := readSrc(t, rel)

		fnIdx := strings.Index(src, "function loadModule(name)")
		if fnIdx < 0 {
			t.Errorf("%s: could not locate function loadModule(name)", rel)
			continue
		}
		urlIdx := strings.Index(src[fnIdx:], "'/__gofastr/runtime/' + name")
		if urlIdx < 0 {
			t.Errorf("%s: could not locate the module URL construction in loadModule", rel)
			continue
		}
		body := src[fnIdx : fnIdx+urlIdx]

		// A guard counts as an anchored allow-list char class tested
		// against name, with an optional {m,n} or + quantifier and
		// optional regex flags: /^[A-Za-z0-9_-]+$/.test(name).
		guardRe := regexp.MustCompile(`/\^\[([^\]/]+)\](?:\+|\{\d+(?:,\d+)?\})\$[a-z]*/\.test\(name\)`)
		m := guardRe.FindStringSubmatch(body)
		if m == nil {
			t.Errorf("SECURITY: [module-src] %s: loadModule builds '/__gofastr/runtime/'+name+'.js' with no name-shape guard — data-fui-prefetch is DOM input, a ../../../evil token normalizes past the runtime serve route onto an arbitrary same-origin JS path (sibling data-behavior gate: boot.js hydrate)", rel)
			continue
		}
		cls := m[1]
		letters := strings.Contains(cls, "A-Za-z") || strings.Contains(cls, "a-zA-Z") || strings.Contains(cls, "a-z") || strings.Contains(cls, `\w`)
		digits := strings.Contains(cls, "0-9") || strings.Contains(cls, `\w`)
		if !letters || !digits {
			t.Errorf("SECURITY: [module-src] %s: loadModule name guard class [%s] no longer covers the emitted module-name alphabet (letters+digits)", rel, cls)
		}
		for _, bad := range []string{".", ":", "%", " ", `\s`} {
			if strings.Contains(cls, bad) {
				t.Errorf("SECURITY: [module-src] %s: loadModule name guard class [%s] admits %q — traversal or scheme characters must not be allow-listed", rel, cls, bad)
			}
		}
	}
}

// TestSelectorInterpolationEscaped asserts the codebase-wide selector
// hygiene property: every value read from the DOM (or from DOM-borne
// config attributes) and interpolated into a querySelector/closest
// selector string is wrapped in CSS.escape() first.
//
// The runtime already established this convention at eight sites
// (sse.js island names, toasts.js stacks, widgets.js names, panehost.js
// URL keys, sortablelist.js groups, scrollspy.js anchors, runtime.js
// signal names): a value carrying `"]` (or any selector metacharacter)
// either re-targets the lookup at an unintended element or throws
// SyntaxError, which silently kills the enclosing module's wiring. The
// surfaces below interpolate the SAME class of DOM-attribute-borne
// value without the escape, so a server reflecting request input into
// the attribute (the threat TestSseIslandSelectorEscaped documents)
// breaks or re-targets them.
//
// Escape acceptance covers the module-local alias: scrollspy.js wraps
// with cssEscape(), its own CSS.escape shim.
//
// Surfaces are every interpolated selector call site in the shipped
// runtime (runtime.js + src/*); the "escaped" column is the control
// group proving the assertion detects a missing escape, not a missing
// site.
func TestSelectorInterpolationEscaped(t *testing.T) {
	// The escape-window check this probe used to carry inline now
	// lives in core-ui/check.LintSelectorInterpolation, so the
	// property holds over EVERY selector call site in the shipped
	// runtime, not only the ones listed below. The surface table
	// remains as the drift tripwire: every anchor must still exist
	// (renamed attribute or rewritten lookup ⇒ update the table
	// deliberately), and the "escaped" control-group entries document
	// the sites the audit fixed, whose escapes the lint must keep
	// seeing.
	surfaces := []struct {
		file   string
		anchor string // unique literal at the selector call site
		where  string // human-readable surface description
	}{
		{"src/conditionalfield.js", `'[name="'`, "data-when-name value → [name=…] lookup"},
		{"src/multiselect.js", `label[for="`, "checkbox id → label[for=…] lookup"},
		{"src/carousel.js", `data-fui-carousel-deferred-for="`, "carousel id → manifest script lookup"},
		{"src/carousel.js", `'[data-fui-carousel-defer="`, "manifest key → defer placeholder lookup"},
		{"src/rangeslider.js", `data-fui-range-slider="`, "data-fui-range-slider value → pair lookups (3 selectors)"},
		{"src/slider.js", `output[for="`, "input id → output[for=…] lookup"},
		{"src/widgets.js", `link[data-fui-style="`, "widget name → style-link dedup lookup"},
		{"runtime.js", `link[data-fui-style="`, "component name → style-link dedup lookup (composed from frag/kernel.js)"},
		{"runtime.js", `[data-widget="${`, "closest data-component/data-widget value → hydrate lookup (composed from frag/boot.js)"},
		// Control group: these sites escape today and must keep doing so.
		{"src/sse.js", `'[data-island="'`, "island name lookup (pinned by TestSseIslandSelectorEscaped)"},
		{"src/toasts.js", `'[data-fui-toast-stack="'`, "toast stack name lookup"},
		{"src/sortablelist.js", `data-fui-sortable-group="`, "sortable group lookup"},
		{"src/panehost.js", `'[data-fui-pane-key="'`, "URL-borne pane key lookup"},
		{"src/scrollspy.js", `'#' + cssEscape(`, "anchor id lookup (module-local cssEscape shim)"},
		{"src/widgets.js", `'[data-fui-widget="'`, "widget name → mounted-widget lookup"},
		{"src/widgets.js", `'[data-fui-backdrop="'`, "widget name → backdrop lookup"},
		{"runtime.js", `'[data-fui-signal="'`, "signal name → consumer fanout lookup"},
	}

	for _, s := range surfaces {
		src := readSrc(t, s.file)
		if !strings.Contains(src, s.anchor) {
			t.Fatalf("could not locate selector anchor %q in %s — source drifted, update the surface table", s.anchor, s.file)
		}
	}

	// The property itself, over the whole runtime (frag/ + src/; the
	// generated runtime.js is pinned byte-identical to the fragments by
	// TestComposedRuntimeMatchesOnDiskFile).
	res, err := check.LintSelectorInterpolation(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [selector-injection] unescaped interpolated selector(s) in the shipped runtime:\n%s",
			strings.TrimSpace(res.Error()))
	}

	// Vacuity control: the pre-fix spelling (conditionalfield.js at
	// 7bd789e9) must still fire the lint, so a quiet result above
	// means the code is clean, not that the lint went blind.
	vdir := t.TempDir()
	vfile := filepath.Join(vdir, "vacuity.js")
	vsrc := "function wireField(form, whenName) {\n" +
		"  return form.querySelectorAll('[name=\"' + whenName + '\"]');\n" +
		"}\n"
	if err := os.WriteFile(vfile, []byte(vsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintSelectorInterpolation(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintSelectorInterpolation no longer fires on the pre-fix '[name=\"' + whenName spelling — the probe above cannot detect a regression")
	}
}

// TestRegistryLookupsAreOwnProps extends the family pinned by
// TestComputedReducerOwnPropOnly: a registry keyed by an attribute-borne
// name must be read as an OWN property, never through the prototype
// chain. `_widgetCatalog` / `_widgets` are plain `{}` objects, so an
// attribute value like "constructor" resolves to Object.prototype
// members: the truthiness gate passes, `entry.cfg` is undefined, and
// _mountByName throws (widget never opens), while `NS._widgets[name]`
// truthiness makes openWidget treat "constructor"/"toString" as
// already-mounted. The fix idiom is the one computed.js already uses,
// Object.prototype.hasOwnProperty.call(reg, name).
//
// Surfaces: every read of these registries keyed by a DOM-attribute
// variable (data-fui-open / data-fui-widget / data-fui-rpc-refresh /
// data-fui-popover names). Writes keyed by cfg.name (catalog-borne)
// are out of scope.
func TestRegistryLookupsAreOwnProps(t *testing.T) {
	// The nearby-hasOwnProperty window check this probe used to carry
	// inline now lives in core-ui/check.LintRegistryOwnProps, enforced
	// over every {} registry read in the shipped runtime. The needle
	// table remains as the drift tripwire: every lookup must still
	// exist (a renamed registry or refactored read ⇒ update the table
	// deliberately).
	needles := []struct {
		file, needle string
	}{
		{"src/widgets.js", `NS._widgetCatalog[name]`},
		{"src/widgets.js", `NS._widgets[name]`},
		{"src/rpc.js", `NS._widgets[widgetName]`},
		{"src/rpc.js", `NS._widgets[refreshName]`},
		{"src/popover.js", `NS._widgets[name]`},
		{"src/widgetfocus.js", `G._widgets[name]`},
	}

	for _, n := range needles {
		src := readSrc(t, n.file)
		if !strings.Contains(src, n.needle) {
			t.Fatalf("could not locate registry lookup %q in %s — source drifted, update the surface table", n.needle, n.file)
		}
	}

	// The property itself, over the whole runtime.
	res, err := check.LintRegistryOwnProps(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [registry-lookup] prototype-chain registry read(s) in the shipped runtime:\n%s",
			strings.TrimSpace(res.Error()))
	}

	// Vacuity control: the pre-fix spelling (rpc.js at 7bd789e9) must
	// still fire the lint.
	vdir := t.TempDir()
	decls := "window.__gofastr = window.__gofastr || {};\n" +
		"window.__gofastr._widgets = window.__gofastr._widgets || {};\n"
	readers := "const NS = window.__gofastr;\n" +
		"function rpcRefresh(widgetName) {\n" +
		"  const wentry = NS._widgets && NS._widgets[widgetName];\n" +
		"  return wentry;\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(vdir, "decls.js"), []byte(decls), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vdir, "readers.js"), []byte(readers), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintRegistryOwnProps(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintRegistryOwnProps no longer fires on the pre-fix NS._widgets[widgetName] spelling — the probe above cannot detect a regression")
	}
}

// TestResponseHTMLMountedOnlyAfterOK asserts every runtime path that
// mounts fetched text via innerHTML first checks the response status.
// The convention is explicit elsewhere: poll.js's comment requires that
// "an HTTP error must reach .catch", and rpc.js/intercept.js/infinitescroll.js
// all gate on !r.ok before consuming the body. A path that skips the
// check mounts whatever the server (or an intercepting proxy) returns
// for an error — error pages routinely reflect the request URL and
// attacker-influenced path segments, so mounting them as page markup
// turns reflected error output into injected HTML.
//
// Surfaces: every response-text → innerHTML site in the shipped
// modules. sortablelist's conflict-recovery path is the one that skips
// the status check today: dest.innerHTML = html runs for any status,
// including the 4xx/5xx body, replacing the column with an error page.
func TestResponseHTMLMountedOnlyAfterOK(t *testing.T) {
	// The fetch→mount span check this probe used to carry inline now
	// lives in core-ui/check.LintResponseMountedAfterOK, enforced over
	// every fetch chain in the shipped runtime. The surface table
	// remains as the drift tripwire: both anchors of every span must
	// still exist.
	surfaces := []struct {
		file     string
		from, to string // unique literals bracketing the fetch→mount span
		where    string
	}{
		{"src/sortablelist.js", "fetch(crpc", "dest.innerHTML = html", "conflict-recovery refresh"},
		{"src/infinitescroll.js", "await fetch(path", "tmp.innerHTML = html", "infinite-scroll append"},
		{"src/poll.js", "fetch(src", "el.innerHTML = html", "poll region swap"},
		{"src/intercept.js", "fetch(path", "mount(res.html", "intercept overlay mount"},
	}

	for _, s := range surfaces {
		src := readSrc(t, s.file)
		from := strings.Index(src, s.from)
		if from < 0 {
			t.Fatalf("could not locate fetch anchor %q in %s — source drifted, update the surface table", s.from, s.file)
		}
		if to := strings.Index(src[from:], s.to); to < 0 {
			t.Fatalf("could not locate mount anchor %q after the fetch in %s — source drifted, update the surface table", s.to, s.file)
		}
	}

	// The property itself, over the whole runtime.
	res, err := check.LintResponseMountedAfterOK(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [response-html] fetch chain(s) that mount response text without an .ok check:\n%s",
			strings.TrimSpace(res.Error()))
	}

	// Vacuity control: the pre-fix conflict-recovery chain
	// (sortablelist.js at 7bd789e9) must still fire the lint.
	vdir := t.TempDir()
	vsrc := "function conflictRefresh(dest, crpc) {\n" +
		"  fetch(crpc, { credentials: 'same-origin' })\n" +
		"    .then(function (r) { return r.text(); })\n" +
		"    .then(function (html) { dest.innerHTML = html; })\n" +
		"    .catch(function () {});\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"), []byte(vsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintResponseMountedAfterOK(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintResponseMountedAfterOK no longer fires on the pre-fix .text()→innerHTML chain — the probe above cannot detect a regression")
	}
}

// TestAttributePathSegmentsValidated extends the family pinned by
// TestModuleSrcValidatesNameShape: a value read from a DOM attribute
// and interpolated into a URL PATH must be validated against the shape
// the framework emits before the request fires, because the browser
// normalizes `../` segments and re-targets the request onto any
// same-origin route (past the handler that owns the prefix).
//
// loadModule's name gate is the control surface (already pinned).
// _kilnPost builds '/kiln/tool/' + data-kiln-tool verbatim: on a kiln
// build-mode page (body.kiln-app) any injected markup carrying
// data-kiln-tool="../../admin/…" POSTs attacker JSON to an arbitrary
// same-origin route, with the page's CSRF token attached. Kiln tool
// names are emitted as [A-Za-z0-9_-]+, so an anchored shape check
// rejects nothing legitimate.
//
// Surface: _kilnPost in src/rpc.js (the only remaining attribute-borne
// path build in the runtime's fetch surface).
func TestAttributePathSegmentsValidated(t *testing.T) {
	// The kilnPost gate check is kept inline (it pins the exact gate
	// spelling on the one known surface), and the general property —
	// no attribute-borne value joins a URL path ungated, anywhere in
	// the runtime — now lives in
	// core-ui/check.LintAttributePathSegments.
	src := readSrc(t, "src/rpc.js")
	start := strings.Index(src, "const _kilnPost")
	if start < 0 {
		t.Fatal("could not locate _kilnPost in src/rpc.js — source drifted, update this test")
	}
	body := src[start:]
	if end := strings.Index(body, "_dispatchKiln"); end > 0 {
		body = body[:end]
	}
	if !strings.Contains(body, "fetch(") {
		t.Fatal("could not locate the fetch inside _kilnPost — source drifted, update this test")
	}
	// The gate shape the module loader uses: an anchored allow-list of
	// the emitted name alphabet, applied between the attribute read and
	// the fetch.
	if !strings.Contains(body, "[A-Za-z0-9_-]") {
		t.Errorf("SECURITY: [attr-path] src/rpc.js: _kilnPost builds '/kiln/tool/' + data-kiln-tool with no name-shape validation — a tool value carrying '../' re-targets the POST onto any same-origin route with the page's CSRF token; gate the name like loadModule does (see TestModuleSrcValidatesNameShape)")
	}

	// The property itself, over the whole runtime.
	res, err := check.LintAttributePathSegments(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [attr-path] attribute-borne value(s) joined into URL paths ungated:\n%s",
			strings.TrimSpace(res.Error()))
	}

	// Vacuity control: the pre-fix _kilnPost (rpc.js at 7bd789e9)
	// must still fire the lint.
	vdir := t.TempDir()
	vsrc := "const _kilnPost = (el, body) =>\n" +
		"  fetch('/kiln/tool/' + el.getAttribute('data-kiln-tool'), {\n" +
		"    method: 'POST',\n" +
		"    body,\n" +
		"  }).catch(() => {});\n"
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"), []byte(vsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintAttributePathSegments(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintAttributePathSegments no longer fires on the pre-fix '/kiln/tool/' + data-kiln-tool spelling — the probe above cannot detect a regression")
	}
}

// ---------------------------------------------------------------------------
// Behavioral pins (gadget_e2e_test.go harness). The properties they pin
// are also enforced shape-wide by the source scans below; these drive a
// real browser so the DOM behaviour (cookie set, pane classes, signal
// value) is what fails, not just the spelling.
// ---------------------------------------------------------------------------

// TestBannerDismissCookieEncodesId pins recordDismiss's storage boundary:
// values concatenated into document.cookie from data-fui-* attributes are
// component-encoded first, so a DOM-sourced dismiss id can never
// contribute cookie delimiters. Without the encoding, a crafted
// data-fui-banner-dismiss-id 'probe=x; Path=' plants an
// attacker-chosen name/value pair inside the gofastr.banner-dismiss.*
// namespace the server reads back (and the real dismissal is never stored
// under its own key).
func TestBannerDismissCookieEncodesId(t *testing.T) {
	// Benign banner at boot gives the marker scanner its load trigger;
	// the crafted banner is injected post-boot, the reachable shape for
	// attribute injection (island swap / RPC innerHTML / SPA merge).
	g := startGadgetServer(t, `[]`, `
<div data-fui-comp="ui-banner" id="bn0">
  <button type="button" id="dismiss0" data-fui-banner-dismiss
          data-fui-banner-dismiss-id="boot-marker">x</button>
</div>`)

	ctx := newSeedBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.banner&&window.__gofastr.banner.rescan)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`(function () {
			var bn = document.createElement('div');
			bn.setAttribute('data-fui-comp', 'ui-banner');
			var b = document.createElement('button');
			b.type = 'button';
			b.setAttribute('data-fui-banner-dismiss', '');
			b.setAttribute('data-fui-banner-dismiss-id', 'probe=x; Path=/');
			bn.appendChild(b);
			document.body.appendChild(bn);
			b.click();
			var want = 'gofastr.banner-dismiss.' + encodeURIComponent('probe=x; Path=/');
			var names = document.cookie.split(';')
				.map(function (c) { return c.trim().split('=')[0]; })
				.filter(function (n) { return n.indexOf('gofastr.banner-dismiss.') === 0; });
			return JSON.stringify({ names: names, want: want });
		})()`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Names []string `json:"names"`
		Want  string   `json:"want"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	for _, n := range got.Names {
		if n != got.Want {
			t.Errorf("SECURITY: [banner-cookie] crafted dismiss id planted cookie %q; only the encoded name %q is legitimate — recordDismiss must component-encode the data-fui-banner-dismiss-id into the cookie name so the id cannot inject cookie attributes", n, got.Want)
		}
	}
}

// TestPaneHostCraftedValueNoOp pins the pane name from
// data-fui-pane-open/-close/-swap as DOM input: a crafted value must not
// retarget openPane at another pane, plant non-canonical classes, or
// throw out of the delegated click listener. paneEl escapes the value
// with CSS.escape (the family rule its own pane-key lookup set).
func TestPaneHostCraftedValueNoOp(t *testing.T) {
	// Tertiary first in document order: a two-branch crafted selector
	// must not resolve to it.
	g := startGadgetServer(t, `[]`, `
<div data-fui-pane-host id="ph1">
  <div data-fui-pane="tertiary" id="pane-ter" hidden>T</div>
  <div data-fui-pane="secondary" id="pane-sec" hidden>S</div>
</div>
<div data-fui-pane-host id="ph2">
  <div data-fui-pane="secondary" id="pane-sec2" hidden>S2</div>
</div>`)

	ctx := newSeedBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.panehost)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`(function () {
			window.__paneErr = '';
			window.addEventListener('error', function (e) { window.__paneErr = e.message || String(e); });
			var b1 = document.createElement('button');
			b1.setAttribute('data-fui-pane-open', 'secondary"], [data-fui-pane="tertiary');
			document.getElementById('ph1').appendChild(b1);
			var b2 = document.createElement('button');
			b2.setAttribute('data-fui-pane-open', 'b"ad');
			document.getElementById('ph2').appendChild(b2);
			b1.click();
			b2.click();
			var bad = [];
			if (!document.getElementById('pane-ter').hasAttribute('hidden')) bad.push('wrong pane opened (tertiary)');
			if (!document.getElementById('pane-sec').hasAttribute('hidden')) bad.push('secondary opened');
			if (!document.getElementById('pane-sec2').hasAttribute('hidden')) bad.push('ph2 secondary opened');
			var h = document.getElementById('ph1');
			for (var i = 0; i < h.classList.length; i++) {
				var c = h.classList[i];
				if (c !== 'ui-pane-host' && !/^ui-pane-host--(secondary|tertiary)-open$/.test(c)) {
					bad.push('non-canonical class ' + c);
				}
			}
			if (window.__paneErr) bad.push('threw: ' + window.__paneErr);
			return JSON.stringify(bad);
		})()`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	var bad []string
	if err := json.Unmarshal([]byte(raw), &bad); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	if len(bad) > 0 {
		t.Errorf("SECURITY: [panehost-selector] crafted data-fui-pane-open value was interpolated raw into the pane selector: %s — a crafted value must match nothing so the click is a no-op", strings.Join(bad, "; "))
	}
}

// TestRpcScrollSelectorDegradesOnly pins that a post-success UI hint
// (data-fui-rpc-scroll-to) degrades without corrupting the RPC result:
// the hint's querySelector runs in its own try after the response signal
// is set, so a malformed selector can never overwrite a successful
// response signal with the network-error object.
func TestRpcScrollSelectorDegradesOnly(t *testing.T) {
	g := startGadgetServer(t, `[]`, `
<button type="button" id="rpcbtn" data-fui-rpc="/rpcok" data-fui-rpc-method="POST"
        data-fui-rpc-signal="result" data-fui-rpc-scroll-to="[[">go</button>
<span id="sig" data-fui-signal="result"></span>`)

	ctx := newSeedBrowserCtx(t)
	var raw string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(g.Srv.URL+"/"),
		chromedp.WaitVisible(`#ready`, chromedp.ByID),
		chromedp.Poll(`!!(window.__gofastr&&window.__gofastr.loadedModules&&window.__gofastr.loadedModules.rpc)`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Click(`#rpcbtn`, chromedp.ByID),
		chromedp.Poll(`window.__gofastr && window.__gofastr.getSignal && window.__gofastr.getSignal('result') !== undefined`, nil,
			chromedp.WithPollingTimeout(10*time.Second), chromedp.WithPollingInterval(50*time.Millisecond)),
		chromedp.Evaluate(`(function () {
			var s = window.__gofastr.getSignal('result');
			if (s && typeof s === 'object') {
				return JSON.stringify({ object: true, ok: s.ok, status: s.status, text: String(s.text || '') });
			}
			return JSON.stringify({ object: false, type: typeof s });
		})()`, &raw),
	); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Object bool   `json:"object"`
		OK     *bool  `json:"ok"`
		Status *int   `json:"status"`
		Text   string `json:"text"`
	}
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("probe returned %q: %v", raw, err)
	}
	if got.Object && got.OK != nil && !*got.OK {
		t.Errorf("SECURITY: [rpc-scroll-selector] successful RPC overwritten with the error object {ok:%v,status:%v,text:%q} — the data-fui-rpc-scroll-to lookup must run in its own try after the response signal is set, so a malformed selector degrades without touching the result", *got.OK, *got.Status, got.Text)
	}
}

// ---------------------------------------------------------------------------
// Runtime-shape enforcement. These three scans are the runtime-side
// twins of the source lints in core-ui/check (LintSelectorInterpolation,
// LintAttributePathSegments, …): one property per recurring bug SHAPE,
// run over every shipped module source (src/ + frag/ — the composed
// runtime.js is pinned byte-identical by TestComposedRuntimeMatches-
// OnDiskFile), each with a vacuity control holding the pre-fix spelling
// that must keep firing.
// ---------------------------------------------------------------------------

// jsRuntimeSourceFiles returns every module and fragment source the
// runtime ships, relative to the package dir.
func jsRuntimeSourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, dir := range []string{"src", "frag"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".js") {
				files = append(files, filepath.Join(dir, e.Name()))
			}
		}
	}
	return files
}

// reAttrRead matches a DOM attribute read: getAttribute('data-…') in
// both quote spellings, and .dataset.member (dot or bracket).
var reAttrRead = regexp.MustCompile(
	`getAttribute\s*\(\s*['"]data-[A-Za-z0-9_-]+['"]\s*\)` +
		`|\.\s*dataset\s*\.\s*\w+` +
		`|\.\s*dataset\s*\[\s*['"][A-Za-z0-9_-]+['"]\s*\]`)

// reAttrVarAssign matches a `const|let|var NAME = RHS` declaration whose
// RHS carries an attribute read (same line): NAME is attribute-borne.
var reAttrVarAssign = regexp.MustCompile(
	`(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*([^\n;]*)`)

// attrBorneIdents returns the identifiers in src assigned from a DOM
// attribute read. Those identifiers carry attacker-influenceable strings
// into every later use, which is what the three scans key on.
func attrBorneIdents(src string) map[string]bool {
	out := map[string]bool{}
	for _, m := range reAttrVarAssign.FindAllStringSubmatch(src, -1) {
		if reAttrRead.MatchString(m[2]) {
			out[m[1]] = true
		}
	}
	return out
}

// reBareSelectorCall matches a querySelector(All) whose whole argument
// is one identifier.
var reBareSelectorCall = regexp.MustCompile(
	`(querySelectorAll|querySelector)\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*\)`)

// selectorByDesignFinding is one unguarded attribute-borne selector
// lookup: file, line, and the identifier that reached it raw.
type selectorByDesignFinding struct {
	file  string
	line  int
	ident string
}

// scanSelectorByDesign reports every querySelector(All) call whose bare
// argument is an attribute-borne identifier and that does not sit inside
// a try block. For the data-fui-* attributes whose VALUE IS a selector
// by design (copy-text-from, fill-input, charcount-source,
// shortcut-target, rpc-scroll-to, scroll-bottom-on-update, btt-target,
// infinite-items, scrollspy, scrollspy-target, toc) escaping is wrong —
// the contract is that a malformed selector degrades to a no-op instead
// of throwing a DOMException out of the delegated handler, before its
// preventDefault.
func scanSelectorByDesign(file, src string) []selectorByDesignFinding {
	borne := attrBorneIdents(src)
	var out []selectorByDesignFinding
	for _, m := range reBareSelectorCall.FindAllStringSubmatchIndex(src, -1) {
		ident := src[m[4]:m[5]]
		if !borne[ident] {
			continue
		}
		// The call's '(' position must sit inside a try block.
		paren := strings.IndexByte(src[m[3]:m[4]], '(')
		if insideTryBlock(src, m[3]+paren) {
			continue
		}
		out = append(out, selectorByDesignFinding{
			file:  file,
			line:  strings.Count(src[:m[2]], "\n") + 1,
			ident: ident,
		})
	}
	return out
}

// insideTryBlock reports whether pos sits inside a try block: some
// enclosing block's opening brace directly follows the try keyword.
// (Untagged twin of the red-probe helper; innermostOpenerBefore and
// skipSpaceBack are defined above.)
func insideTryBlock(s string, pos int) bool {
	for {
		i := innermostOpenerBefore(s, pos)
		if i < 0 {
			return false
		}
		j := skipSpaceBack(s, i-1)
		// The block's last non-space char before '{' must be the 'y' of
		// "try" (a 3-byte word ending at j; the byte before it must not
		// extend the identifier — "retry {" is not a try block).
		if j >= 2 && s[j-2:j+1] == "try" {
			if k := j - 3; k < 0 || !identByte(s[k]) {
				return true
			}
		}
		pos = i
	}
}

// TestSelectorByDesignLookupsGuarded enforces the degrade-don't-throw
// contract over every selector-by-design lookup in the shipped runtime.
// The fixes wrapped each site in try/catch (or validated) so a malformed
// data-fui-* selector cannot abort the delegated click/keydown/wire
// handler that carries it.
//
// Surfaces (drift tripwire — every anchor must keep existing):
//   - src/copy.js            data-fui-copy-text-from
//   - src/widgethelpers.js   data-fui-fill-input, data-fui-charcount-source
//   - src/shortcut.js        data-fui-shortcut-target
//   - src/rpc.js             data-fui-rpc-scroll-to
//   - frag/signals.js        data-fui-scroll-bottom-on-update
//   - src/backtotop.js       data-fui-btt-target
//   - src/infinitescroll.js  data-fui-infinite-items
//   - src/scrollspy.js       data-fui-scrollspy, data-fui-scrollspy-target
//   - src/toc.js             data-fui-toc
func TestSelectorByDesignLookupsGuarded(t *testing.T) {
	anchors := []struct{ file, anchor string }{
		{"src/copy.js", `document.querySelector(sel)`},
		{"src/widgethelpers.js", `widget.querySelector(sel)`},
		{"src/widgethelpers.js", `sel && document.querySelector(sel)`},
		{"src/shortcut.js", `el.querySelector(sel)`},
		{"src/rpc.js", `document.querySelector(scrollSel)`},
		{"frag/signals.js", `node.querySelector(sel)`},
		{"src/backtotop.js", `document.querySelector(scrollTarget)`},
		{"src/infinitescroll.js", `wrap.querySelector(itemsSel)`},
		{"src/scrollspy.js", `document.querySelector(observeSel)`},
		{"src/scrollspy.js", `root.querySelectorAll(targetSel)`},
		{"src/toc.js", `document.querySelector(target)`},
	}
	for _, a := range anchors {
		if !strings.Contains(readSrc(t, a.file), a.anchor) {
			t.Fatalf("anchor %q not found in %s — module changed, update the surface table", a.anchor, a.file)
		}
	}

	for _, file := range jsRuntimeSourceFiles(t) {
		for _, f := range scanSelectorByDesign(file, readSrc(t, file)) {
			t.Errorf("SECURITY: [selector-by-design] %s:%d runs the attribute-borne selector lookup querySelector(%s) unguarded — a malformed data-fui-* selector throws a DOMException out of the delegated handler (before its preventDefault); the lookup must be wrapped in try/catch or otherwise validated so it degrades to a no-op", f.file, f.line, f.ident)
		}
	}

	// Vacuity control: the pre-fix spelling (no try) must keep firing,
	// so a quiet result above means the code is clean, not that the
	// scan went blind.
	vsrc := "function wire(el) {\n" +
		"  const sel = el.getAttribute('data-fui-x-target');\n" +
		"  return document.querySelector(sel);\n" +
		"}\n"
	if got := scanSelectorByDesign("vacuity.js", vsrc); len(got) == 0 {
		t.Error("VACUITY: the selector-by-design scan no longer fires on the unguarded pre-fix spelling — the property above cannot detect a regression")
	}
}

// reCookieAssign matches a document.cookie write.
var reCookieAssign = regexp.MustCompile(`document\.cookie\s*=`)

// scanCookieConcat reports every document.cookie assignment that
// interpolates an attribute-borne identifier into the statement without
// component-encoding it first: a DOM-sourced value carrying ';', '=', or
// whitespace plants attacker-chosen cookie attributes (Path, Max-Age,
// Secure, Domain) or a whole extra name=value pair inside the namespace
// the server reads back. Encoding is satisfied inline
// (encodeURIComponent(id)) or through a same-file helper the identifier
// is passed to whose body encodes its parameter.
func scanCookieConcat(file, src string) []int {
	borne := attrBorneIdents(src)
	var out []int
	for _, loc := range reCookieAssign.FindAllStringIndex(src, -1) {
		stmtEnd := strings.IndexByte(src[loc[1]:], '\n')
		region := src[loc[1]:]
		if stmtEnd >= 0 {
			region = region[:stmtEnd]
		}
		encoded := strings.Contains(region, "encodeURIComponent(")
		if encoded {
			continue
		}
		// Helper form: FN(ident) where function FN's body encodes.
		helperOK := false
		for name := range borne {
			if !wordIn(region, name) {
				continue
			}
			if helperEncodes(src, region, name) {
				helperOK = true
				break
			}
		}
		if helperOK {
			continue
		}
		// Any attribute-borne identifier reaching the statement raw?
		for name := range borne {
			if wordIn(region, name) {
				out = append(out, strings.Count(src[:loc[0]], "\n")+1)
				break
			}
		}
	}
	return out
}

// helperEncodes reports whether every occurrence of ident in region is
// as the argument of a call FN(ident) whose same-file declaration's body
// contains encodeURIComponent(.
func helperEncodes(src, region, ident string) bool {
	callRE := regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$]*)\s*\(\s*` + regexp.QuoteMeta(ident) + `\s*\)`)
	calls := callRE.FindAllStringSubmatch(region, -1)
	if len(calls) == 0 {
		// The identifier reaches the cookie statement raw, not through
		// any encoding helper.
		return false
	}
	for _, m := range calls {
		fn := m[1]
		decl := regexp.MustCompile(`function\s+` + regexp.QuoteMeta(fn) + `\s*\([^)]*\)\s*\{`)
		dm := decl.FindStringSubmatchIndex(src)
		if dm == nil {
			return false
		}
		body := src[dm[0]:funcBodyEnd(src, dm[1]-1)]
		if !strings.Contains(body, "encodeURIComponent(") {
			return false
		}
	}
	return true
}

// funcBodyEnd returns the index just past the '}' closing the block that
// opens at open (a '{' offset), skipping string literals naively.
func funcBodyEnd(s string, open int) int {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		case '\'', '"', '`':
			q := s[i]
			for i++; i < len(s) && s[i] != q; i++ {
				if s[i] == '\\' {
					i++
				}
			}
		}
	}
	return len(s)
}

// wordIn reports whether name occurs in s as a whole identifier.
func wordIn(s, name string) bool {
	for off := 0; off+len(name) <= len(s); {
		i := strings.Index(s[off:], name)
		if i < 0 {
			return false
		}
		i += off
		lo := i == 0 || !identByte(s[i-1])
		hi := i+len(name) >= len(s) || !identByte(s[i+len(name)])
		if lo && hi {
			return true
		}
		off = i + len(name)
	}
	return false
}

func identByte(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// TestCookieWritesEncodeAttrValues pins the cookie-concatenation shape:
// every document.cookie write in the shipped runtime that involves a
// DOM-attribute-borne value component-encodes it first (the banner
// module's dismiss id is the surface; the fix routed both the
// localStorage key and the cookie name through one encodeURIComponent
// boundary).
func TestCookieWritesEncodeAttrValues(t *testing.T) {
	for _, file := range jsRuntimeSourceFiles(t) {
		src := readSrc(t, file)
		for _, line := range scanCookieConcat(file, src) {
			t.Errorf("SECURITY: [cookie-concat] %s:%d writes document.cookie interpolating an attribute-borne value without encodeURIComponent — a crafted data-fui-* value can inject cookie delimiters ('; '=') and plant attacker-chosen cookie attributes or name/value pairs", file, line)
		}
	}

	// Vacuity control: the pre-fix banner spelling must keep firing.
	vsrc := "function recordDismiss(id) {\n" +
		"  document.cookie = 'gofastr.banner-dismiss.' + id + '=1; path=/; max-age=31536000; SameSite=Lax';\n" +
		"}\n" +
		"function click(btn) {\n" +
		"  const id = btn.getAttribute('data-fui-banner-dismiss-id');\n" +
		"  if (id) recordDismiss(id);\n" +
		"}\n"
	if got := scanCookieConcat("vacuity.js", vsrc); len(got) == 0 {
		t.Error("VACUITY: the cookie-concat scan no longer fires on the pre-fix raw-id concatenation — the property above cannot detect a regression")
	}
}

// reSrcAssign matches an Element src assignment.
var reSrcAssign = regexp.MustCompile(`\.src\s*=`)

// reNameShapeGate matches an anchored name-shape regex test against
// name: /^[\w…]+$/-style literals only, so a gate that can admit a path
// separator (dots allowed for component ids) is judged by its own
// spelling.
var reNameShapeGate = regexp.MustCompile(
	`/\^\[?\\w[A-Za-z0-9_.\[\]-]*\]?\+\$/\s*\.\s*test\s*\(\s*([A-Za-z_$][A-Za-z0-9_$]*)`)

// scanModuleURLShape reports every src assignment whose right-hand side
// concatenates an attribute-borne identifier with a URL-ish string
// literal while no anchored name-shape gate on that identifier precedes
// it in the file: the browser normalizes '../' segments in a script src
// and re-targets the load onto any same-origin route. loadModule
// (frag/boot.js) is the pinned control — its /^[\w-]+$/ gate is the
// parity actionloader now carries for its dotted ids.
func scanModuleURLShape(file, src string) []int {
	borne := attrBorneIdents(src)
	// Identifier → gate exists somewhere before each use.
	gated := map[string][]int{}
	for _, m := range reNameShapeGate.FindAllStringSubmatchIndex(src, -1) {
		name := src[m[2]:m[3]]
		gated[name] = append(gated[name], m[0])
	}
	var out []int
	for _, loc := range reSrcAssign.FindAllStringSubmatchIndex(src, -1) {
		lineEnd := strings.IndexByte(src[loc[1]:], '\n')
		region := src[loc[1]:]
		if lineEnd >= 0 {
			region = region[:lineEnd]
		}
		for name := range borne {
			if !wordIn(region, name) {
				continue
			}
			if !strings.Contains(region, "'/") && !strings.Contains(region, "\"/") && !strings.Contains(region, "`/") {
				continue
			}
			ok := false
			for _, gpos := range gated[name] {
				if gpos < loc[0] {
					ok = true
					break
				}
			}
			if !ok {
				out = append(out, strings.Count(src[:loc[0]], "\n")+1)
			}
		}
	}
	return out
}

// TestModuleURLsGateAttrNames pins the module-URL shape: a script src
// built from a data-fui-* attribute id carries the same anchored
// name-shape gate loadModule applies to its names — parity within one
// runtime, the second line of defense behind the server-controlled
// manifest.
func TestModuleURLsGateAttrNames(t *testing.T) {
	// Drift tripwire: actionloader's build + gate must both keep
	// existing (the gate dots-allowed variant, component ids carry
	// dots).
	aloader := readSrc(t, filepath.Join("src", "actionloader.js"))
	if !strings.Contains(aloader, `'/__gofastr/widget/' + id`) {
		t.Fatalf("src construction anchor not found in src/actionloader.js — module changed, update this pin")
	}
	if !strings.Contains(aloader, `/^[\w.-]+$/`) {
		t.Fatalf("name-shape gate anchor /^w.-]+$/ not found in src/actionloader.js — module changed, update this pin")
	}

	for _, file := range jsRuntimeSourceFiles(t) {
		src := readSrc(t, file)
		for _, line := range scanModuleURLShape(file, src) {
			t.Errorf("SECURITY: [module-url-shape] %s:%d builds an element src from an attribute-borne id with no anchored name-shape gate before it — the browser normalizes '../' segments and re-targets the load onto any same-origin route; gate the id like loadModule's /^[\\w-]+$/ (actionloader: dots allowed)", file, line)
		}
	}

	// Vacuity control: the pre-fix actionloader spelling (no gate) must
	// keep firing.
	vsrc := "const scan = (root) => {\n" +
		"  for (const el of root.querySelectorAll('[data-component]')) {\n" +
		"    const id = el.getAttribute('data-component');\n" +
		"    const s = document.createElement('script');\n" +
		"    s.src = '/__gofastr/widget/' + id + '.js';\n" +
		"    document.head.appendChild(s);\n" +
		"  }\n" +
		"};\n"
	if got := scanModuleURLShape("vacuity.js", vsrc); len(got) == 0 {
		t.Error("VACUITY: the module-url scan no longer fires on the pre-fix ungated src build — the property above cannot detect a regression")
	}
}

// TestSelectorBareArgGuarded: a querySelector whose argument is a bare
// identifier read from a data-fui-* attribute runs inside a try, so a
// malformed value degrades instead of throwing out of the delegated
// handler. Property over the whole runtime; vacuity control on the
// pre-fix copy.js spelling.
func TestSelectorBareArgGuarded(t *testing.T) {
	res, err := check.LintSelectorBareArgGuarded(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [selector-bare-arg] unguarded attribute-borne selector lookup(s) in the shipped runtime:\n%s",
			strings.TrimSpace(res.Error()))
	}
	vdir := t.TempDir()
	vsrc := "function onClick(el) {\n" +
		"  const sel = el.getAttribute('data-fui-copy-text-from');\n" +
		"  const src = document.querySelector(sel);\n" +
		"  return src;\n}\n"
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"), []byte(vsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintSelectorBareArgGuarded(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintSelectorBareArgGuarded no longer fires on the pre-fix bare querySelector(sel) spelling")
	}
}

// TestCookieWritesEncodeOperands: document.cookie writes never splice a
// raw non-literal operand (the banner dismiss id used to). Vacuity
// control on the pre-fix banner.js spelling.
func TestCookieWritesEncodeOperands(t *testing.T) {
	res, err := check.LintCookieConcat(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [cookie-concat] raw operand in a document.cookie write in the shipped runtime:\n%s",
			strings.TrimSpace(res.Error()))
	}
	vdir := t.TempDir()
	vsrc := "const STORAGE_PREFIX = 'gofastr.banner-dismiss.';\n" +
		"function recordDismiss(id) {\n" +
		"  document.cookie = STORAGE_PREFIX + id + '=1; Path=/; Max-Age=31536000; SameSite=Lax';\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"), []byte(vsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintCookieConcat(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintCookieConcat no longer fires on the pre-fix STORAGE_PREFIX + id spelling")
	}
}

// TestModuleURLsGateTheirId: a script src or dynamic import built from a
// DOM-sourced id is dominated by a regex shape test (runtime.js
// loadModule is the pattern). Vacuity control on the pre-fix
// actionloader.js spelling.
func TestModuleURLsGateTheirId(t *testing.T) {
	res, err := check.LintModuleURLShape(".")
	if err != nil {
		t.Fatal(err)
	}
	if res.HasErrors() {
		t.Errorf("SECURITY: [module-url-shape] ungated module URL build in the shipped runtime:\n%s",
			strings.TrimSpace(res.Error()))
	}
	vdir := t.TempDir()
	vsrc := "function scan(root, manifest) {\n" +
		"  root.querySelectorAll('[data-widget]').forEach(function (el) {\n" +
		"    const id = el.getAttribute('data-widget');\n" +
		"    const s = document.createElement('script');\n" +
		"    s.src = '/__gofastr/widget/' + id + '.js?v=' + manifest[id];\n" +
		"    document.head.appendChild(s);\n" +
		"  });\n}\n"
	if err := os.WriteFile(filepath.Join(vdir, "vacuity.js"), []byte(vsrc), 0o644); err != nil {
		t.Fatal(err)
	}
	vres, err := check.LintModuleURLShape(vdir)
	if err != nil {
		t.Fatal(err)
	}
	if !vres.HasErrors() {
		t.Error("VACUITY: LintModuleURLShape no longer fires on the pre-fix '/__gofastr/widget/' + id spelling")
	}
}
