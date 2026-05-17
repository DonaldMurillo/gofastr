package runtime

import (
	"strings"
	"testing"
)

func TestRuntimeJS(t *testing.T) {
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}
	if len(js) == 0 {
		t.Fatal("runtime JS is empty")
	}
	// Check essential features are present
	checks := []string{
		"__gofastr",
		"register",
		"trigger",
		"data-action",
		"data-component",
		"MutationObserver",
		"EventSource",
		"data-island",
		"hydrate",
		"collectParams",
		"screenCache",        // screen caching for back-navigation
		"swapMainContent",    // partial content swapping
		"X-Gofastr-Navigate", // client-side navigation header
		"X-Gofastr-Partial",  // server partial response header
		"loadComponentCSS",   // per-component CSS loader
		"scanAndLoadCSS",     // marker scan post-swap/post-mount
		"_pendingLinks",      // sync dedup guard
		"data-fui-style",     // <link> dedup key
		"scheduleIdleLoads",  // LoadPrewarm idle queue
		"data-fui-comp",      // marker attr the scanner reads
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
	if !strings.HasPrefix(trimmed, "(() =>") && !strings.HasPrefix(trimmed, "(function") {
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
		"data-fui-fileupload",         // marker the scanner reads
		"input[type=\"file\"]",        // wires the inner native input
		"DataTransfer",                // drop path
		"FileReader",                  // image thumbnail
		"__gofastr.scanFileUploads",   // exposed scanner for SPA re-wire
		"loadedModules",               // self-registers as loaded
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
