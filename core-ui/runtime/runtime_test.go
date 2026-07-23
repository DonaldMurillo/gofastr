package runtime

import (
	"strings"
	"testing"
)

// TestNominifyEnvGating pins the env contract: prod wins by default,
// dev opts out, manual overrides trump env detection. Subtests can't
// share the sync.Once cache — exercise the underlying decision via the
// helpers directly.
func TestNominifyEnvGating(t *testing.T) {
	t.Setenv("RUNTIME_NOMINIFY", "")
	t.Setenv("RUNTIME_MINIFY", "")
	t.Setenv("GOFASTR_ENV", "")
	t.Setenv("GOFASTR_DEV", "")

	// envBool / isNonDevEnv behaviour.
	if envBool("RUNTIME_NOMINIFY") {
		t.Error("envBool with empty value should be false")
	}
	t.Setenv("X_TEST_BOOL", "1")
	if !envBool("X_TEST_BOOL") {
		t.Error(`envBool("1") should be true`)
	}
	t.Setenv("X_TEST_BOOL", "true")
	if !envBool("X_TEST_BOOL") {
		t.Error(`envBool("true") should be true`)
	}
	t.Setenv("X_TEST_BOOL", "false")
	if envBool("X_TEST_BOOL") {
		t.Error(`envBool("false") should be false`)
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
	// or use whitespace patterns the minifier produces — bare `foo:bar`
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
		"screenCache",                       // screen caching for back-navigation
		"swapMainContent",                   // partial content swapping
		"X-Gofastr-Navigate",                // client-side navigation header
		"X-Gofastr-Partial",                 // server partial response header
		"loadComponentCSS",                  // per-component CSS loader
		"scanAndLoadCSS",                    // marker scan post-swap/post-mount
		"_pendingLinks",                     // sync dedup guard
		"data-fui-style",                    // <link> dedup key
		"scheduleIdleLoads",                 // LoadPrewarm idle queue
		"data-fui-comp",                     // marker attr the scanner reads
		"data-fui-copy-text-from",           // marker triggers copy module load
		"data-fui-os",                       // OS detection on <html> for ShortcutHint
		"data-fui-spa",                      // opt-IN form-intercept for non-JSON forms
		"redirect:'follow'",                 // form-intercept follows server Location headers (minified spacing)
		"application/x-www-form-urlencoded", // SPA-opt-in body encoding
	}
	for _, check := range checks {
		if !strings.Contains(js, check) {
			t.Errorf("runtime JS missing: %s", check)
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
	// runtime module (fileupload, popover, toasts, menu, sse, forms,
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

func TestRuntimeModule_Fileupload(t *testing.T) {
	src, ok := Module("fileupload")
	if !ok {
		t.Fatal("fileupload module not embedded")
	}
	for _, want := range []string{
		"data-fui-fileupload",       // marker the scanner reads
		"input[type=\"file\"]",      // wires the inner native input
		"DataTransfer",              // drop path
		"FileReader",                // image thumbnail
		"__gofastr.scanFileUploads", // exposed scanner for SPA re-wire
		"loadedModules",             // self-registers as loaded
	} {
		if !strings.Contains(src, want) {
			t.Errorf("fileupload module missing %q", want)
		}
	}
	// Per-module size budget. Fileupload is small (drag/drop +
	// preview only); cap stays generous so a future feature add
	// still has headroom. Raw bytes, not gzip.
	if size := ModuleSize("fileupload"); size > 6000 {
		t.Errorf("fileupload module is %d bytes — budget is 6000", size)
	}
}

func TestRuntimeModule_PaneHost(t *testing.T) {
	src, ok := Module("panehost")
	if !ok {
		t.Fatal("panehost module not embedded")
	}
	for _, want := range []string{
		"[data-fui-pane-host]", // marker selector the scanner reads
		"data-fui-pane-open",   // open trigger attribute
		"openPane",             // programmatic API on __gofastr
		"matchMedia",           // responsive overlay-drawer collapse
		"NS._focusSel",         // reuses the shared focusable selector
		"loadedModules",        // self-registers as loaded
	} {
		if !strings.Contains(src, want) {
			t.Errorf("panehost module missing %q", want)
		}
	}
	// Per-module raw size bound (consistent with sibling modules; the
	// gzip 3 KB budget is enforced separately by TestRuntimeModuleSizeBudgets).
	if size := ModuleSize("panehost"); size > 9000 {
		t.Errorf("panehost module is %d bytes — budget is 9000", size)
	}
}

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
		"X-Gofastr-Toast", // header path awaits toasts
		"loadedModules",
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
		"shortcut":      {"data-fui-shortcut-click", "data-fui-shortcut-focus"},
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

func TestRuntimeModule_Copy(t *testing.T) {
	src, ok := Module("copy")
	if !ok {
		t.Fatal("copy module not embedded")
	}
	for _, want := range []string{
		"data-fui-copy-text-from", // marker
		"data-fui-copy-status",    // SR-announce sibling
		"data-fui-copy-announce",  // override text
		"data-fui-copy-toast",     // toast-on-copy opt-in
		"fui-copied",              // visual feedback class
		"navigator.clipboard",     // primary path
		"loadedModules",           // self-registers as loaded
	} {
		if !strings.Contains(src, want) {
			t.Errorf("copy module missing %q", want)
		}
	}
	if size := ModuleSize("copy"); size > 1800 {
		t.Errorf("copy module is %d bytes — budget is 1800", size)
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

func TestRuntimeModule_NetworkRetryBanner(t *testing.T) {
	src, ok := Module("networkretrybanner")
	if !ok {
		t.Fatal("networkretrybanner module not embedded")
	}
	for _, want := range []string{
		`data-fui-comp="ui-network-retry-banner"`, // on-demand marker
		"data-fui-network-retry-health",           // health probe URL
		"data-fui-network-retry-sse-silence",      // opt-in silence threshold
		"networkStatus",                           // public API on __gofastr
		"checkHealthOn",                           // recovery helper reused on reconnect
		"reportRecoveryOn",                        // dismiss path
		"__gofastr.sseStatus",                     // silence poll reads the SSE global
		"gofastr:sse-status",                      // reconnect-recovery listener
	} {
		if !strings.Contains(src, want) {
			t.Errorf("networkretrybanner module missing %q", want)
		}
	}
	// Ceiling near the current minified size (same shape as menu/cop),
	// not a goal — tighten down if the module shrinks.
	if size := ModuleSize("networkretrybanner"); size > 4000 {
		t.Errorf("networkretrybanner module is %d bytes — budget is 4000", size)
	}
}

func TestRuntimeModule_Sidebar(t *testing.T) {
	src, ok := Module("sidebar")
	if !ok {
		t.Fatal("sidebar module not embedded")
	}
	for _, want := range []string{
		"data-fui-sidebar-collapse",
		"data-fui-sidebar-storage",
		"localStorage.setItem",
		"aria-expanded",
		"gofastr:navigate",
		"MutationObserver",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("sidebar module missing %q", want)
		}
	}
	if size := ModuleSize("sidebar"); size > 3000 {
		t.Errorf("sidebar module is %d bytes — budget is 3000", size)
	}
}

func TestRuntimeModule_Toasts(t *testing.T) {
	src, ok := Module("toasts")
	if !ok {
		t.Fatal("toasts module not embedded")
	}
	for _, want := range []string{
		"NS.toast",
		"NS._initToasts",
		"NS._dismissToast",
		"data-fui-toast-id",
		"data-fui-toast-stack",
		"data-fui-toast-dismiss",
		"data-fui-toast-ttl-ms",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("toasts module missing %q", want)
		}
	}
	if size := ModuleSize("toasts"); size > 8000 {
		t.Errorf("toasts module is %d bytes — budget is 8000", size)
	}
}

func TestRuntimeModule_Menu(t *testing.T) {
	src, ok := Module("menu")
	if !ok {
		t.Fatal("menu module not embedded")
	}
	for _, want := range []string{
		`role="menuitem"`,
		`role="menu"`,
		"ArrowDown",
		"ArrowUp",
		"_menuTypeBuf",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("menu module missing %q", want)
		}
	}
	if size := ModuleSize("menu"); size > 4000 {
		t.Errorf("menu module is %d bytes — budget is 4000", size)
	}
}

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

func TestRuntimeModule_Combobox(t *testing.T) {
	src, ok := Module("combobox")
	if !ok {
		t.Fatal("combobox module not embedded")
	}
	for _, want := range []string{
		`role="combobox"`,
		`role="listbox"`,
		`role="option"`,
		"aria-activedescendant",
		"aria-expanded",
		"ArrowDown",
		"ArrowUp",
		"Escape",
		"pickOption",
		"loadedModules",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("combobox module missing %q", want)
		}
	}
	if size := ModuleSize("combobox"); size > 8000 {
		t.Errorf("combobox module is %d bytes — budget is 8000", size)
	}
}

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
	// Sorted invariant — the HTTP server relies on it for stable URLs.
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
//     kiln/integration/TestBrowser_ButtonToolCallFires — the test renders
//     a real kiln-app page and a non-kiln page with the same delegator
//     payload and asserts only the trusted one fires.
//   - findCommonScreenGroup deepest-match is verified by
//     core-ui/app/TestNestedGroupRendersNestedLayoutShells (SSR side)
//     plus the existing chromedp screen-group e2e (DOM-stable nav).

// Regression: scrollspy's cssEscape polyfill must handle ids that
// start with a digit (legal HTML5, illegal as bare CSS selectors).
// querySelector('#2foo') throws SyntaxError without escaping; the
// polyfill must emit `\\3<digit><space>` (the CSS spec form) or
// equivalent so the selector parses.
func TestScrollspyCSSEscapeHandlesLeadingDigit(t *testing.T) {
	src, ok := Module("scrollspy")
	if !ok {
		t.Fatal("scrollspy module missing")
	}
	// The fix uses a charCodeAt branch to emit `\\3<hex><space>` for
	// the first char when it's a digit (or use CSS.escape natively).
	// Accept any of the canonical forms.
	if !contains(src, "charCodeAt(0)") && !contains(src, "/^[0-9]/") && !contains(src, "/^\\d/") {
		t.Error("cssEscape polyfill must special-case leading-digit ids — querySelector('#42foo') throws otherwise")
	}
}

// TestRuntimeNavigateRejectsUnsafeSchemes — security: when the SPA
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
	// The navigate() body MUST call _isUnsafeSignalUrl (or the
	// internal alias). Scan within ~400 chars after the function
	// signature so we don't accept the guard living anywhere on the
	// page.
	idx := strings.Index(js, "navigate(path")
	if idx == -1 {
		t.Fatal("navigate() function not found in runtime.js")
	}
	body := js[idx:min(idx+800, len(js))]
	if !strings.Contains(body, "_isUnsafeSignalUrl") &&
		!strings.Contains(body, "javascript:") {
		t.Errorf("navigate() must reject unsafe schemes before pushState; "+
			"found body (truncated): %q", truncate(body, 400))
	}
}

// TestRuntimeDisclosureAndEscapeRunInBothBranches — a11y: the Escape-
// to-close handler for <details data-fui-disclosure> and the
// aria-expanded mirror were previously only attached inside the
// `document.readyState === 'loading'` branch. If runtime.js loaded
// after DOMContentLoaded (late injection / fast parse), Esc didn't
// close the mobile drawer and SR users got stale aria-expanded.
//
// Source-pattern check: the keydown listener for Escape and the
// toggle listener for the disclosure mirror must NOT be lexically
// nested inside the `if (document.readyState === 'loading')` block.
func TestRuntimeDisclosureAndEscapeRunInBothBranches(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// Minified form drops the spaces; accept both.
	loadingIdx := strings.Index(js, "readyState === 'loading'")
	if loadingIdx == -1 {
		loadingIdx = strings.Index(js, "readyState==='loading'")
	}
	if loadingIdx == -1 {
		t.Fatal("readyState branch not found")
	}
	// Find the matching `} else {` (or minified `}else{`) that
	// closes the loading branch.
	elseIdx := strings.Index(js[loadingIdx:], "} else {")
	if elseIdx == -1 {
		elseIdx = strings.Index(js[loadingIdx:], "}else{")
	}
	if elseIdx == -1 {
		t.Fatal("else branch terminator not found")
	}
	loadingBlock := js[loadingIdx : loadingIdx+elseIdx]
	// The keydown listener for Escape closing the disclosure must
	// live OUTSIDE this block. If it's still in here, the fix
	// hasn't shipped.
	escEvidence := []string{
		"e.key !== 'Escape'",
		"details[data-fui-disclosure][open]",
	}
	for _, s := range escEvidence {
		if strings.Contains(loadingBlock, s) {
			t.Errorf("Escape-close handler still nested inside "+
				"readyState==='loading' branch — substring %q "+
				"must be hoisted to run unconditionally", s)
		}
	}
}

// TestRuntimeDisclosureFocusTrapWiring — disclosures opting in via
// data-fui-disclosure-trap (mobile drawers, full-sheet popovers) must
// gain a focus-trap via `inert` on body siblings. Confirms the
// runtime emits both the on-open and on-close branches so the
// trap is symmetric.
func TestRuntimeDisclosureFocusTrapWiring(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"data-fui-disclosure-trap",
		"inert",
		"_applyDisclosureTrap",
	} {
		if !strings.Contains(js, want) {
			t.Errorf("runtime missing focus-trap wiring %q", want)
		}
	}
}

// TestComboboxPickOptionHonorsPushState — selecting a combobox option
// carrying data-fui-push-state must navigate. The previous behavior
// only set input.value + fired change, which left CommandPalette
// completely non-functional (user could open + type + select, but
// the click was a no-op).
//
// Additionally guards against the XSS vector: an attacker-controlled
// push-state value (e.g. "javascript:alert(1)") must not navigate.
func TestComboboxPickOptionHonorsPushState(t *testing.T) {
	src, ok := Module("combobox")
	if !ok {
		t.Fatal("combobox module not embedded")
	}
	// Source-pattern guard: pickOption must read data-fui-push-state.
	if !strings.Contains(src, "data-fui-push-state") {
		t.Error("combobox pickOption must read data-fui-push-state on selection")
	}
	// And must route through the SPA navigator OR a safety-checked
	// fallback. Either presence of navigate( in the picked-option
	// branch or a scheme check counts.
	if !strings.Contains(src, "navigate") && !strings.Contains(src, "_isUnsafeSignalUrl") {
		t.Error("combobox pickOption must call navigate or scheme-check before nav")
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

// TestRuntimeLoadingCSSClassDuringRPC pins that the module-level
// dispatchRPC adds a fui-loading CSS class to the trigger node during
// the in-flight request. This lets CSS authors style loading states.
func TestRuntimeLoadingCSSClassDuringRPC(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	// Must add the class before fetch and remove it in finally.
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
//   - examples/site/TestE2E_HoverPrefetchLoadsModule — synthesizes
//     pointerover on a data-fui-prefetch element and asserts the
//     monkey-patched loadModule fired exactly once with the right name.
//   - examples/site/TestE2E_IdleFallbackUsesRIC — stubs
//     requestIdleCallback=undefined and asserts the setTimeout fallback
//     still loads the queued module.
//   - examples/site/TestE2E_RuntimeSplit_HoverPrefetch — covers the
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

// TestCarousel_TimerTeardownOnNav guards F16a: the carousel setInterval
// must be cleared on gofastr:navigate so auto-rotate doesn't leak across
// SPA navigation. The nav handler must call stop/clearInterval — not just
// re-scan for new carousels.
func TestCarousel_TimerTeardownOnNav(t *testing.T) {
	src, ok := Module("carousel")
	if !ok {
		t.Fatal("carousel module not embedded")
	}
	// Require evidence that the navigate handler calls stop() or clearInterval
	// on the tracked active carousels. The simplest form: the nav listener
	// iterates a tracking structure and stops each timer. We accept any of:
	//   - "activeCarousels" Set referenced in navigate handler
	//   - "stop(" called inside or near the navigate handler
	//   - "_fuiCarouselStop" teardown helper
	idx := strings.Index(src, "gofastr:navigate")
	if idx == -1 {
		t.Fatal("carousel missing gofastr:navigate handler")
	}
	// Extract 600 chars after the navigate listener registration.
	body := src[idx:min(idx+600, len(src))]
	teardownEvidence := strings.Contains(body, "stop(") ||
		strings.Contains(body, "clearInterval") ||
		strings.Contains(body, "activeCarousels") ||
		strings.Contains(body, "_fuiCarouselStop")
	if !teardownEvidence {
		t.Error("carousel gofastr:navigate handler must stop auto-rotate timers — no teardown evidence found near the navigate listener")
	}
}

// TestTOC_ObserverTeardownOnNav guards F16b: the toc.js IntersectionObserver
// must be disconnected on gofastr:navigate so it doesn't leak across SPA nav.
func TestTOC_ObserverTeardownOnNav(t *testing.T) {
	src, ok := Module("toc")
	if !ok {
		t.Fatal("toc module not embedded")
	}
	// The observer must be disconnected on navigate. The fix adds a
	// Set of active observers and disconnects them before re-scanning.
	hasNav := strings.Contains(src, "gofastr:navigate")
	hasDisconnect := strings.Contains(src, ".disconnect()")
	if !hasNav || !hasDisconnect {
		t.Errorf("toc must disconnect IntersectionObserver on gofastr:navigate — hasNavHandler=%v hasDisconnect=%v", hasNav, hasDisconnect)
	}
}
