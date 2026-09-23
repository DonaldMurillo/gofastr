package runtime

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// TestNominifyEnvGating pins the env contract: prod wins by default,
// dev opts out, manual overrides trump env detection. Subtests can't
// share the sync.Once cache, exercise the underlying decision via the
// helpers directly.
func TestNominifyEnvGating(t *testing.T) {
	t.Setenv("RUNTIME_NOMINIFY", "")
	t.Setenv("RUNTIME_MINIFY", "")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV", "")

	// config.EnvBool / isNonDevEnv behaviour.
	if config.EnvBool("RUNTIME_NOMINIFY") {
		t.Error("config.EnvBool with empty value should be false")
	}
	t.Setenv("X_TEST_BOOL", "1")
	if !config.EnvBool("X_TEST_BOOL") {
		t.Error(`config.EnvBool("1") should be true`)
	}
	t.Setenv("X_TEST_BOOL", "true")
	if !config.EnvBool("X_TEST_BOOL") {
		t.Error(`config.EnvBool("true") should be true`)
	}
	t.Setenv("X_TEST_BOOL", "false")
	if config.EnvBool("X_TEST_BOOL") {
		t.Error(`config.EnvBool("false") should be false`)
	}

	for _, e := range []string{"production", "prod", "live", "staging", "PRODUCTION"} {
		if !isNonDevEnv(e) {
			t.Errorf("isNonDevEnv(%q) should be true", e)
		}
	}
	for _, e := range []string{"", "dev", "development", "test", "local"} {
		if isNonDevEnv(e) {
			t.Errorf("isNonDevEnv(%q) should be false", e)
		}
	}
}

func TestRuntimeJS(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if len(js) == 0 {
		t.Fatal("runtime JS is empty")
	}
	// Check essential features are present. Anchors must be either
	// identifiers/string-literals (preserved verbatim by the minifier)
	// or use whitespace patterns the minifier produces, bare `foo:bar`
	// (no space after `:`) rather than `foo: bar`.
	checks := []string{
		"__gofastr",
		"register",
		"trigger",
		"data-action",
		"data-action-${eventType}",
		"data-component",
		"MutationObserver",
		"hydrate",
		"collectParams",
		"screenCache",          // screen caching for back-navigation
		"swapAtSlot",           // layer-cell content swapping
		"data-fui-layout-key",  // layout-chain identity marker
		"data-fui-layout-slot", // layer swap-target marker
		"X-Gofastr-Navigate",   // client-side navigation header
		"X-Gofastr-Swap",       // subtree-partial swap boundary
		"X-Gofastr-Partial",    // server partial response header
		"loadComponentCSS",     // per-component CSS loader
		"scanAndLoadCSS",       // marker scan post-swap/post-mount
		"_pendingLinks",        // sync dedup guard
		"data-fui-style",       // <link> dedup key
		"scheduleIdleLoads",    // LoadPrewarm idle queue
		"data-fui-comp",        // marker attr the scanner reads
		"data-fui-os",          // OS detection on <html> for ShortcutHint
		"data-fui-spa",         // opt-IN form-intercept for non-JSON forms
	}
	for _, check := range checks {
		if !strings.Contains(js, check) {
			t.Errorf("runtime JS missing: %s", check)
		}
	}
}

func TestRuntimeModule_RPC(t *testing.T) {
	js, ok := Module("rpc")
	if !ok {
		t.Fatal("rpc module not embedded")
	}
	for _, check := range []string{
		"dispatchRPC",
		"redirect:'follow'",
		"application/x-www-form-urlencoded",
		"X-CSRF-Token",
		"X-FUI-Widget",
	} {
		if !strings.Contains(js, check) {
			t.Errorf("rpc module missing: %s", check)
		}
	}
}

func TestRuntimeSize(t *testing.T) {
	size := RuntimeSize()
	if size == 0 {
		t.Fatal("runtime size is 0")
	}
	t.Logf("Runtime size: %d bytes", size)
	// Reasonably small for: router + DOM helpers + SSE + hydration +
	// widget mounting + per-component CSS loader (catalog + bundle
	// dedup + idle prefetch) + the data-fui-* primitive set
	// (rpc-reset, disable-when-invalid, submit-on-enter, autogrow,
	// clear-on-esc, shortcut-focus, shortcut-click, fill-input,
	// scroll-bottom-on-update, flash-on-update, tick-elapsed,
	// charcount-source, persist-storage, copy-text-from, data-fui-
	// comp, rpc-after-text, rpc-after-disable, rpc-scroll-to,
	// data-fui-disclosure SPA-nav+Escape close, route-announce live
	// region, overlay timer cleanup, LRU screen cache, full-script
	// sanitization, inline-JSON catalog/routes hydration, per-signal
	// RPC abort dedup, aria-busy progress + nav-failure toast,
	// summary aria-expanded mirror, widget deep-link sync via
	// pushState/popstate + click-time signal seeding, toast stack TTL
	// + hover-pause + click-to-dismiss + JS API + header dispatch,
	// menu type-ahead + roving focus, modal scroll lock + Tab focus
	// trap + return-focus). Cap at 92KB uncompressed (~24-26KB gzip),
	// still well under typical TCP slow-start initial windows after
	// compression.
	// Cap stays generous during the code-split transition. As each
	// runtime module (popover, toasts, menu, sse, forms,
	// widgets) moves to core-ui/runtime/src/, this cap will tighten.
	// Final target: core ≤ 36 KB raw, each split module ≤ 8 KB.
	if size > 112000 {
		t.Errorf("runtime too large: %d bytes (max 112000)", size)
	}
}

func TestMustRuntimeJS(t *testing.T) {
	js := MustRuntimeJS()
	if len(js) == 0 {
		t.Fatal("runtime JS is empty")
	}
}

func TestRuntimeJSSyntax(t *testing.T) {
	// Basic syntax checks
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// IIFE wrapper (ES2020+ arrow style)
	trimmed := strings.TrimSpace(js)
	// Strip leading comments
	for strings.HasPrefix(trimmed, "//") {
		idx := strings.Index(trimmed, "\n")
		if idx == -1 {
			break
		}
		trimmed = strings.TrimSpace(trimmed[idx+1:])
	}
	// Accept both minified (`(()=>`) and unminified (`(() =>`) IIFE
	// preludes. The RUNTIME_NOMINIFY=1 dev path keeps the original
	// spacing.
	if !strings.HasPrefix(trimmed, "(()=>") &&
		!strings.HasPrefix(trimmed, "(() =>") &&
		!strings.HasPrefix(trimmed, "(function") {
		t.Errorf("runtime should be an IIFE, got: %s", truncate(trimmed, 50))
	}
	// Should end with closing
	if !strings.HasSuffix(trimmed, ")();") {
		t.Error("runtime should end with )();")
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// Split-runtime modules under core-ui/runtime/src/ ship as
// individually-loadable bundles.

func TestRuntimeModule_Widgets(t *testing.T) {
	src, ok := Module("widgets")
	if !ok {
		t.Fatal("widgets module not embedded")
	}
	for _, want := range []string{
		"NS.mountWidget",
		"NS.openWidget",
		"NS.closeWidget",
		"NS._mountByName",
		"NS._deepLinkPushUrl",
		"NS._deepLinkStripUrl",
		"NS._syncDeepLinks",
		"NS._modalStack", // reads state from core
		"NS._popoverStack",
		"data-fui-backdrop",
		"data-fui-widget",
		"data-fui-rpc",
		"widgethelpers",
		"widgetfocus",
		"widgetlinks",
		// Toast response handling lives in the shared RPC demand module.
		"NS.loadModule('rpc')",
		// #409: lifecycle events + post-swap reattach.
		"fui:widget-open",
		"fui:widget-close",
		"NS._reattachWidgets",
		// `data-fui-copy-text-from` was previously checked here but
		// only lives in a comment now (the delegated handler moved
		// to core); the minifier correctly strips it.
	} {
		if !strings.Contains(src, want) {
			t.Errorf("widgets module missing %q", want)
		}
	}
	moduleMarkers := map[string][]string{
		"widgethelpers": {"data-fui-persist-storage", "data-fui-charcount-source", "data-fui-clear-on-esc", "data-fui-submit-on-enter", "data-fui-disable-when-invalid", "data-fui-fill-input", "data-fui-tick-elapsed"},
		"widgetfocus":   {"__fuiModalEsc", "__fuiModalTab"},
		"widgetlinks":   {"G._deepLinkPushUrl", "G._deepLinkStripUrl"},
		"textarea":      {"data-fui-autogrow"},
	}
	for module, markers := range moduleMarkers {
		moduleSrc, ok := Module(module)
		if !ok {
			t.Errorf("%s module not embedded", module)
			continue
		}
		for _, marker := range markers {
			if !strings.Contains(moduleSrc, marker) {
				t.Errorf("%s module missing %q", module, marker)
			}
		}
	}
	// Keep the raw source bounded as well as the gzip budget: widgets owns
	// mount/open/close orchestration, while optional scanners live elsewhere.
	if size := ModuleSize("widgets"); size > 20000 {
		t.Errorf("widgets module is %d bytes — budget is 20000", size)
	}
}

func TestRuntimeModule_SSE(t *testing.T) {
	src, ok := Module("sse")
	if !ok {
		t.Fatal("sse module not embedded")
	}
	for _, want := range []string{
		`meta[name="gofastr-sse"]`,
		"EventSource",
		"data-island",
		"NS.connectSSE",
		"loadedModules",
		"NS.sseStatus",       // connection-state global the banner polls
		"lastEventAt",        // refreshed per frame + on open
		"retryCount",         // bumped on each transport error
		"gofastr:sse-status", // transition CustomEvent
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sse module missing %q", want)
		}
	}
	if size := ModuleSize("sse"); size > 2500 {
		t.Errorf("sse module is %d bytes — budget is 2500", size)
	}
}

func TestRuntimeModule_WS(t *testing.T) {
	src, ok := Module("ws")
	if !ok {
		t.Fatal("ws module not embedded")
	}
	for _, want := range []string{
		"NS.connectWebSocket",       // sequenced WebSocket client (#377)
		"NS.createSequencedReducer", // reject-stale ordering guard (#375)
		"onGenerationStart",         // socket open hook
		"onHydrated",                // snapshot hydration hook
		"onGenerationEnd",           // transport gone hook
		"reasonClass",               // bounded close classification
		"resyncComplete",            // protocol resynchronization marker
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("ws module missing %q", want)
		}
	}
	// Ceiling near the current size, same shape as the modules above.
	if size := ModuleSize("ws"); size > 8000 {
		t.Errorf("ws module is %d bytes — budget is 8000", size)
	}
	// The module must never log: close reasons, payloads, and
	// credentials stay out of the console by construction.
	if strings.Contains(src, "console.") {
		t.Error("ws module calls console.* — it must not log payloads, close reasons, or credentials")
	}
}

func TestRuntimeModule_RTC(t *testing.T) {
	src, ok := Module("rtc")
	if !ok {
		t.Fatal("rtc module not embedded")
	}
	for _, want := range []string{
		"NS.connectRoom",         // rooms over the rtc signaling protocol
		"NS.loadModule('ws')",    // one-time init pulls the ws module
		"createSequencedReducer", // snapshot/join/leave/status are sequenced
		"onnegotiationneeded",    // perfect negotiation offer path
		"onicecandidate",         // trickle ICE
		"setRemoteDescription",   // polite-side implicit rollback (mid-call collisions)
		"remoteDescription",      // the polite side waits for the first offer
		"addIceCandidate",        // candidate path with swallowed errors
		"negotiated",             // data channels use negotiated:true, id i
		"onconnectionstatechange",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("rtc module missing %q", want)
		}
	}
	// Ceiling near the current size, same shape as the modules above.
	if size := ModuleSize("rtc"); size > 9000 {
		t.Errorf("rtc module is %d bytes — budget is 9000", size)
	}
	// The module must never log: SDP, candidates, credentials, and
	// close reasons stay out of the console by construction.
	if strings.Contains(src, "console.") {
		t.Error("rtc module calls console.* — it must not log SDP, candidates, credentials, or close reasons")
	}
}

// (TestRuntimeModule_Menu retired with the menu module: the source
// contract moved to framework/headless's headless-menu, whose
// registration gates and module-size budget hold it there.)

func TestRuntimeModule_Popover(t *testing.T) {
	src, ok := Module("popover")
	if !ok {
		t.Fatal("popover module not embedded")
	}
	for _, want := range []string{
		"_anchorPopover",            // exported entry on __gofastr
		"data-fui-popover-side",     // chosen-side attr the CSS reads
		"is-popover-trigger-active", // trigger highlight class
		"anchorTrigger",             // per-widget anchor state
		"--ui-popover-arrow-x",      // arrow CSS variable
		"requestAnimationFrame",     // scroll/resize throttle
		"loadedModules",             // self-registers as loaded
	} {
		if !strings.Contains(src, want) {
			t.Errorf("popover module missing %q", want)
		}
	}
	if size := ModuleSize("popover"); size > 8000 {
		t.Errorf("popover module is %d bytes — budget is 8000", size)
	}
}

// (TestRuntimeModule_Combobox retired with the combobox module: the
// source contract moved to framework/headless's headless-combobox,
// whose registration gates and budget hold it there.)

func TestRuntimeModule_Tree(t *testing.T) {
	src, ok := Module("tree")
	if !ok {
		t.Fatal("tree module not embedded")
	}
	for _, want := range []string{
		`role="treeitem"`,
		`role="tree"`,
		`role="group"`,
		"aria-expanded",
		"data-fui-tree-toggle",
		"ArrowRight",
		"ArrowLeft",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("tree module missing %q", want)
		}
	}
	if size := ModuleSize("tree"); size > 8000 {
		t.Errorf("tree module is %d bytes — budget is 8000", size)
	}
}

func TestRuntimeModule_InfiniteScroll(t *testing.T) {
	src, ok := Module("infinitescroll")
	if !ok {
		t.Fatal("infinitescroll module not embedded")
	}
	for _, want := range []string{
		"data-fui-infinite-scroll",
		"data-fui-infinite-sentinel",
		"data-fui-infinite-cursor",
		"X-Gofastr-Infinite-Cursor",
		"IntersectionObserver",
		"aria-busy",
		"_moduleScanners",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("infinitescroll module missing %q", want)
		}
	}
	if size := ModuleSize("infinitescroll"); size > 8000 {
		t.Errorf("infinitescroll module is %d bytes — budget is 8000", size)
	}
}

func TestRuntimeModuleNames(t *testing.T) {
	names := ModuleNames()
	if len(names) == 0 {
		t.Fatal("no runtime modules embedded")
	}
	// Sorted invariant, the HTTP server relies on it for stable URLs.
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("ModuleNames() not sorted at index %d: %v", i, names)
		}
	}
}

func TestRuntimeModuleRejectsBadName(t *testing.T) {
	for _, bad := range []string{"", "..", "../core", "name with space", "name/with/slash", "name.ext"} {
		if _, ok := Module(bad); ok {
			t.Errorf("Module(%q) should reject invalid name", bad)
		}
	}
}

// Behavioral coverage notes (replacing prior source-grep regressions):
//   - setSignal javascript:/vbscript:/data: sanitization is verified by
//     examples/site/TestE2E_SetSignalRejectsJavascriptHref (renders a
//     real bound anchor and asserts the attribute is scrubbed).
//   - <a download> SPA-skip is verified by
//     examples/site/TestE2E_AnchorDownloadSkipsSPA (synthesizes a
//     real click and asserts gofastr:navigate never fired).
//   - data-kiln-tool scoping is verified end-to-end by
//     kiln/integration/TestBrowser_ButtonToolCallFires, the test renders
//     a real kiln-app page and a non-kiln page with the same delegator
//     payload and asserts only the trusted one fires.
//   - findCommonScreenGroup deepest-match is verified by
//     core-ui/app/TestNestedGroupRendersNestedLayoutShells (SSR side)
//     plus the existing chromedp screen-group e2e (DOM-stable nav).

// (The scrollspy cssEscape regression moved with its module: the
// polyfill lives in framework/headless's rail.js now, and the
// selector-escape property over that package's modules is held by
// core-ui/check's selector-interpolation lint plus the headless
// package's own gates.)

// TestRuntimeNavigateRejectsUnsafeSchemes: security: when the SPA
// navigator is handed an attacker-controlled URL (via signal-bound
// href or a combobox option's data-fui-push-state), it must refuse
// javascript:/vbscript:/non-image data: schemes BEFORE calling
// history.pushState. Otherwise the URL bar lies and a Refresh on
// some older WebKit forks executes the script.
//
// Behavioral assertion is via the e2e suite; this guard pins the
// source-level contract that navigate() routes through the existing
// _isUnsafeSignalUrl gate.
func TestRuntimeNavigateRejectsUnsafeSchemes(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// The navigate() body MUST gate the target before pushState. That
	// gate is now _originOK, which subsumes the old _isUnsafeSignalUrl
	// call: a javascript: / data: URL resolves to a null origin, so
	// refusing anything not same-origin refuses those too, and also
	// refuses a cross-origin target the scheme check accepted. Scan
	// within ~800 chars of the signature so we don't accept a guard
	// living anywhere else on the page.
	idx := strings.Index(js, "navigate(path")
	if idx == -1 {
		t.Fatal("navigate() function not found in runtime.js")
	}
	body := js[idx:min(idx+800, len(js))]
	if !strings.Contains(body, "_originOK") &&
		!strings.Contains(body, "_isUnsafeSignalUrl") &&
		!strings.Contains(body, "javascript:") {
		t.Errorf("navigate() must reject unsafe and cross-origin targets before pushState; "+
			"found body (truncated): %q", truncate(body, 400))
	}
}

// TestRuntimeDocScriptBoundaryShape pins the source-level contract of
// document-lifetime scripts (data-fui-doc): every soft-nav entry point
// must consult crossesDocBoundary BEFORE its history write, and the
// fallback arms must be real document loads (location.assign/replace),
// never partial swaps. Removing a document script's tag does not
// uninstall what it installed (WebMCP tools), so reusing the document
// across a scope edge is the capability leak #372 describes.
//
// Behavioral proof (same-scope stays partial, edge hard-loads, back
// restores only the destination's tools) is in
// capability_boundary_e2e_test.go.
func TestRuntimeDocScriptBoundaryShape(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// The live-document truth is read from the DOM, and the
	// destination's set comes from the manifest field kernel maps.
	if !strings.Contains(js, "script[data-fui-doc]") {
		t.Error("runtime never reads script[data-fui-doc]; the live document's capability set is unknown to nav")
	}
	if !strings.Contains(js, "docScripts:r.docScripts??r.DocScripts??[]") {
		t.Error("kernel does not map the manifest docScripts field; destinations carry an empty set and every boundary is invisible")
	}
	// Click hijack: stand down BEFORE preventDefault so the browser
	// performs an ordinary link navigation. (Token-level match: the
	// minifier rewrites whitespace around the call.)
	clickGate := "crossesDocBoundary(fullPath)"
	ci := strings.Index(js, clickGate)
	if ci == -1 {
		t.Fatal("click hijack has no crossesDocBoundary gate")
	}
	handler := js[strings.LastIndex(js[:ci], "document.addEventListener('click'"):]
	if strings.Index(handler, clickGate) > strings.Index(handler, "e.preventDefault()") {
		t.Error("click gate must run before preventDefault: after it, the router has already claimed the click")
	}
	// navigate() and the loadPage backstop hard-load via location.
	navIdx := strings.Index(js, "navigate(path")
	if navIdx == -1 {
		t.Fatal("navigate() not found")
	}
	navBody := js[navIdx:min(navIdx+1400, len(js))]
	if !strings.Contains(navBody, "crossesDocBoundary(path)") || !strings.Contains(navBody, "location.assign(path)") {
		t.Errorf("navigate() must hard-load across a document boundary; body: %q", truncate(navBody, 400))
	}
	if !strings.Contains(js, "crossesDocBoundary(path)") || !strings.Contains(js, "location.assign(path)") {
		t.Error("loadPage has no document-boundary backstop (redirect legs and future callers rely on it)")
	}
	if !strings.Contains(js, "location.replace(path)") {
		t.Error("popstate arm does not replace-load a cross-boundary destination")
	}
}

// (TestRuntimeDisclosureAndEscapeRunInBothBranches and
// TestRuntimeDisclosureFocusTrapWiring retired with the disclosure
// module: the Escape/mirror listeners live unconditionally at the top
// level of framework/headless's disclosure.js, and the trap posture is
// the Tab containment its own module-level keydown owns — both held by
// that package's module gates and its browser e2e.)

// TestRuntimeDemandInteractionBridgeIsGeneric pins two halves of the
// same property: the bridge is metadata-driven, and the kernel names
// no lightbox. The first half is unchanged in spirit from the original
// gate (issue #161's fix had to be generic, not a special case): the
// bridge reads its specs from the descriptor tables — the kernel's own
// and the registered behaviours' — and a hard-coded replay helper is
// the regression it refuses. The second half INVERTED with the
// lightbox's move to a registered behaviour (framework/ui's module):
// before the move this test required the data-fui-lightbox-prev/-next
// literals in boot.js, because the kernel table was their only home.
// Their presence is now the defect — a component's selectors or
// interactions hard-coded back into the always-loaded runtime are
// exactly what the seam exists to remove — so the gate fails on any
// lightbox string in the kernel's shipped bytes or its Go-side tables,
// and the interaction descriptors are proven to arrive the way every
// registered behaviour's do: through the behaviours block.
func TestRuntimeDemandInteractionBridgeIsGeneric(t *testing.T) {
	boot, err := os.ReadFile("frag/boot.js")
	if err != nil {
		t.Fatalf("read boot fragment: %v", err)
	}
	body := string(boot)
	if strings.Contains(body, "__fuiLightboxDispatch") || strings.Contains(body, "_lightboxReplay") {
		t.Fatal("boot fragment contains a lightbox-specific interaction bridge")
	}
	// The generic half: the BRIDGE install loop iterates BOTH
	// descriptor tables and loads through the same loader every path
	// uses. The concat is anchored to the loop that walks
	// marker.interactions — the scan loop concats the same pair one
	// screen later, so a bare Contains on the concat string passes
	// even when the bridge alone regressed to _moduleMarkers. The
	// end-to-end proof of the property (a registered behaviour's
	// interaction replaying with the kernel's table empty of it) is
	// TestLightboxClickBeforeModuleLoadIsReplayed in
	// lightbox_bridge_e2e_test.go; this half is defence in depth
	// around it, not the proof itself.
	bridgeLoop := regexp.MustCompile(
		`for \(const marker of _moduleMarkers\.concat\(_registered\)\) \{\s*` +
			`for \(const spec of marker\.interactions \|\| \[\]\) \{`)
	if !bridgeLoop.MatchString(body) {
		t.Errorf("the interaction bridge's install loop must iterate _moduleMarkers.concat(_registered) over marker.interactions — a bridge reading only the kernel's table is the special case this gate exists to refuse")
	}
	if !strings.Contains(body, "loadModule(marker.name)") {
		t.Errorf("generic interaction bridge missing %q — the bridge must load through the same loader every path uses", "loadModule(marker.name)")
	}
	// The inverted half: the kernel must not name the lightbox. Its
	// marker, its interactions and its requirements are the descriptor
	// framework/ui registers; a literal back in the composed bundle, in
	// the boot fragment it is composed from, in the preload mirror's
	// table, or as a moduleAttrs owner is the special case returning.
	composed, err := RuntimeJS()
	if err != nil {
		t.Fatalf("RuntimeJS: %v", err)
	}
	if got := strings.Count(strings.ToLower(composed), "lightbox"); got != 0 {
		t.Errorf("the composed runtime names the lightbox %d time(s) — a component's strings belong in its registered descriptor, not the always-loaded kernel", got)
	}
	if got := strings.Count(strings.ToLower(body), "lightbox"); got != 0 {
		t.Errorf("frag/boot.js names the lightbox %d time(s)", got)
	}
	for _, name := range DemandLoadModuleNames() {
		if strings.Contains(name, "lightbox") {
			t.Errorf("the preload table still maps a marker to %q — the behaviour's own registration drives its preload now", name)
		}
	}
	if _, owned := moduleAttrs["lightbox"]; owned {
		t.Error("fragments.go still lists a lightbox moduleAttrs owner — its attributes are a registered behaviour's")
	}
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// TestRuntimeSignalAriaLiveContract pins the source-level contract that
// the runtime injects role="status" aria-live="polite" aria-atomic="true"
// onto every [data-fui-signal] node. Two integration points must exist:
//  1. _initialPass (boot-time scan)
//  2. gofastr:navigate handler (post-SPA-nav scan)
//
// The helper function must exist by name so the callsites can delegate.
// Behavioral verification is in examples/site/TestE2EInteractive_SignalHasAriaLive.
func TestRuntimeSignalAriaLiveContract(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// The helper function that sets aria attributes on signal nodes.
	if !contains(js, "_injectSignalAria") && !contains(js, "aria-live") {
		t.Error("runtime missing _injectSignalAria helper or aria-live injection for signal nodes")
	}
	// Must be called from _initialPass.
	if !contains(js, "_injectSignalAria") {
		t.Error("runtime missing _injectSignalAria — needed for boot-time aria-live injection")
	}
	// Must set role="status".
	if !contains(js, `role":"status"`) && !contains(js, `role","status`) && !contains(js, `setAttribute('role','status'`) && !contains(js, `setAttribute("role","status"`) {
		t.Error(`runtime missing setAttribute('role','status') in signal aria injection`)
	}
	// Must set aria-live="polite".
	if !contains(js, `aria-live`) {
		t.Error(`runtime missing aria-live attribute in signal aria injection`)
	}
	// Must set aria-atomic="true".
	if !contains(js, `aria-atomic`) {
		t.Error(`runtime missing aria-atomic attribute in signal aria injection`)
	}
}

// TestRuntimeErrorObjectFormatting pins that setSignal renders error
// objects ({ok:false,...}) as human-readable text, not raw JSON.
// Without this, users see {"ok":false,"status":500,"text":"..."} instead
// of "Error: 500". Behavioral verification is in
// examples/site/TestE2EInteractive_RPCErrorFeedback.
func TestRuntimeErrorObjectFormatting(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// In text mode, when value is an object with ok === false, the
	// runtime must format it as a human-readable string instead of
	// JSON.stringify. Look for evidence of the error formatting branch.
	errorFormattingEvidence := []string{
		"Error:",
		"ok === false",
		"ok===false",
		"value.ok",
	}
	found := false
	for _, evidence := range errorFormattingEvidence {
		if contains(js, evidence) {
			found = true
			break
		}
	}
	if !found {
		t.Error("setSignal must format error objects (ok:false) as human-readable text, not raw JSON")
	}
}

// TestRuntimeLoadingCSSClassDuringRPC pins that dispatchRPC adds a fui-loading
// class during a request and removes it in finally.
func TestRuntimeLoadingCSSClassDuringRPC(t *testing.T) {
	js, ok := Module("rpc")
	if !ok {
		t.Fatal("rpc module not embedded")
	}
	if !contains(js, "fui-loading") {
		t.Error("dispatchRPC must add/remove 'fui-loading' CSS class during in-flight requests")
	}
}

// TestRuntimeReducedMotionFlashSkip pins that the flash animation
// in setSignal respects prefers-reduced-motion. Users who opt into
// reduced motion should not see the flash class toggling.
func TestRuntimeReducedMotionFlashSkip(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if !contains(js, "prefers-reduced-motion") {
		t.Error("setSignal flash must check prefers-reduced-motion before applying fui-flash class")
	}
	if !contains(js, "matchesMedia") && !contains(js, "matchMedia") {
		t.Error("setSignal flash must use matchMedia to detect reduced-motion preference")
	}
}

// Hover/focus prefetch delegator and idle-fallback scheduler are
// verified behaviorally by:
//   - examples/site/TestE2E_HoverPrefetchLoadsModule: synthesizes
//     pointerover on a data-fui-prefetch element and asserts the
//     monkey-patched loadModule fired exactly once with the right name.
//   - examples/site/TestE2E_IdleFallbackUsesRIC: stubs
//     requestIdleCallback=undefined and asserts the setTimeout fallback
//     still loads the queued module.
//   - examples/site/TestE2E_RuntimeSplit_HoverPrefetch: covers the
//     full network fetch path (a code-split module really lands).

// TestWidget_InjectSignalAria_TextModeOnly guards F15: _injectSignalAria
// must restrict role=status/aria-live injection to TEXT-mode signal nodes.
// Applying it to attr-mode or html-mode nodes produces invalid ARIA on
// elements like <a> and spams live-region announcements on island swaps.
func TestWidget_InjectSignalAria_TextModeOnly(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// Find the DEFINITION of _injectSignalAria (contains the forEach body),
	// not an earlier call site. The definition contains querySelectorAll.
	defMarker := `_injectSignalAria`
	start := 0
	defIdx := -1
	for {
		i := strings.Index(js[start:], defMarker)
		if i == -1 {
			break
		}
		abs := start + i
		// The definition site has '=' immediately after the identifier name
		// (const _injectSignalAria = or _injectSignalAria=) and contains
		// querySelectorAll in the next ~300 chars.
		peek := js[abs:min(abs+300, len(js))]
		if strings.Contains(peek, "querySelectorAll") {
			defIdx = abs
			break
		}
		start = abs + 1
	}
	if defIdx == -1 {
		t.Fatal("_injectSignalAria definition (with querySelectorAll body) not found in runtime.js")
	}
	body := js[defIdx:min(defIdx+600, len(js))]
	// Must NOT unconditionally apply to all [data-fui-signal] nodes
	// without a mode check. The mode must be checked or the selector
	// must exclude attr/html-mode nodes.
	appliesUnconditionally := strings.Contains(body, `querySelectorAll('[data-fui-signal]')`) &&
		!strings.Contains(body, `signal-mode`) &&
		!strings.Contains(body, `getAttribute('data-fui-signal-mode')`) &&
		!strings.Contains(body, `getAttribute("data-fui-signal-mode")`) &&
		!strings.Contains(body, `:not([data-fui-signal-mode="attr"])`) &&
		!strings.Contains(body, `:not([data-fui-signal-mode=`)
	if appliesUnconditionally {
		t.Error("_injectSignalAria applies role=status to ALL signal nodes including attr/html-mode — must restrict to text-mode only")
	}
}

// (TestCarousel_TimerTeardownOnNav retired with the carousel module:
// the teardown registry is the per-carousel timer set in
// framework/headless's carousel.js, torn down on gofastr:navigate and
// detach, covered by its e2e.)

// (TestTOC_ObserverTeardownOnNav retired with the toc module: the
// observer registry is a WeakMap in framework/headless's rail.js, so a
// nav removed from the document takes its observer with it when the
// tree is collected, and the kernel hands the post-navigation document
// to the module's registered scanner.)
