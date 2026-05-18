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
		"data-fui-copy-status",         // SR-announce sibling for copy-text-from
		"data-fui-copy-announce",       // copy success message override
		"data-fui-infinite-scroll",     // infinite-scroll RPC path
		"data-fui-infinite-sentinel",   // intersection observer target
		"data-fui-infinite-cursor",     // pagination cursor
		"X-Gofastr-Infinite-Cursor",    // end-of-feed signal
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
	// Bumped 110000 → 115000 → 120000 as new components landed:
	//   - CopyButton SR-announce wiring (data-fui-copy-status / -announce)
	//   - ShortcutHint OS-detection (data-fui-os on <html>)
	//   - InfiniteScroll IntersectionObserver wiring (data-fui-infinite-*)
	// Combobox keyboard nav (ArrowUp/Down/Home/End/Enter/Esc/Tab,
	// click-to-pick, focus auto-open, outside-click close) bumped
	// the cap to 130000.
	// Bumped to 135000 after:
	//   - InfiniteScroll fetch-chain drain (auto-fires next page while
	//     sentinel stays in view)
	//   - Combobox open-on-input + outside-click close + global copy
	//     handler moved here from mountWidget
	//   - Tree toggle click flips aria-expanded + hidden on child group
	if size > 135000 {
		t.Errorf("runtime too large: %d bytes (max 135000)", size)
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
