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
	// inline-JSON catalog/routes hydration). Cap at 70KB
	// uncompressed (~17-19KB gzip), still comfortably under typical
	// TCP slow-start initial windows after compression.
	if size > 70000 {
		t.Errorf("runtime too large: %d bytes (max 70000)", size)
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
