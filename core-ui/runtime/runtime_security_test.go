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
