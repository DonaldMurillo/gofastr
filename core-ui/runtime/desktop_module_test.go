package runtime

import (
	"strings"
	"testing"
)

// TestRuntimeModule_Desktop pins the desktop bridge module's shape:
// the transport contract with battery/desktop (marker-free demand
// module, POST to the chokepoint, _dispatch events), the never-log
// discipline, and the "no bare-identifier namespace" emission rule the
// generated bridge.js relies on (`NS.desktop` is installed once here;
// bridge.js only brackets-keys into it).
func TestRuntimeModule_Desktop(t *testing.T) {
	src, ok := Module("desktop")
	if !ok {
		t.Fatal("desktop module not embedded")
	}
	for _, want := range []string{
		"__gofastr_desktop",        // host-injected availability flag
		"/__gofastr/desktop/call/", // the single chokepoint
		"X-CSRF-Token",             // forwarded when the app ships a csrf meta
		"_dispatch",                // native event entry point
		"loadedModules",            // self-registers as loaded
		"NS.desktop",               // namespace the generated bridge.js extends
		"manifest",                 // null until the generated bridge sets it
		"startDrag",                // window-drag entry point (window namespace)
		"data-fui-window-drag",     // the drag-handle attribute the module wires
		"messageHandlers",          // the drag rides the WebView message channel
	} {
		if !strings.Contains(src, want) {
			t.Errorf("desktop module missing %q", want)
		}
	}
	// Names are shape-gated before they can touch a URL (the regex is
	// hoisted into a const; the minifier keeps NAME.test).
	if !strings.Contains(src, "NAME.test(cap)") || !strings.Contains(src, "NAME.test(method)") {
		t.Error("desktop module must validate cap/method names against the anchored identifier class before building the URL")
	}
	// The module must never log payloads; the only console call names
	// the event, not the payload.
	if strings.Contains(src, "console.error('desktop listener failed', payload") {
		t.Error("desktop module logs payloads in listener failures — name only")
	}
	// Per-module raw-size ceiling, same shape as the modules above.
	if size := ModuleSize("desktop"); size > 5000 {
		t.Errorf("desktop module is %d bytes — budget is 5000", size)
	}
	// Structural syntax check (the repo has no JS parser; the ws/menu
	// modules get the same class of structural pin): IIFE prelude,
	// closing, and balanced delimiters. Accept minified and raw
	// spellings.
	trimmed := strings.TrimSpace(src)
	if !strings.HasPrefix(trimmed, "(()=>{'use strict'") && !strings.HasPrefix(trimmed, "(() => {") {
		t.Errorf("desktop module should be an arrow IIFE, got %q", truncate(trimmed, 40))
	}
	if !strings.HasSuffix(trimmed, "})();") {
		t.Error("desktop module should end with })();")
	}
	for _, pair := range [][2]string{{"{", "}"}, {"(", ")"}, {"[", "]"}} {
		if strings.Count(src, pair[0]) != strings.Count(src, pair[1]) {
			t.Errorf("desktop module unbalanced %q: %d open vs %d close", pair[0], strings.Count(src, pair[0]), strings.Count(src, pair[1]))
		}
	}
}

// TestRuntimeModule_DesktopWindowID pins the cross-window additions:
// windowID is exposed on the namespace, and every call carries the
// X-Gofastr-Window header the server reads to tell which window is the
// caller (windows.broadcast exclusion, windows.self).
func TestRuntimeModule_DesktopWindowID(t *testing.T) {
	src, ok := Module("desktop")
	if !ok {
		t.Fatal("desktop module not embedded")
	}
	for _, want := range []string{
		"windowID",                 // exposed on the namespace
		"X-Gofastr-Window",         // sent on every call
		"__gofastr_desktop.window", // read from the host marker
	} {
		if !strings.Contains(src, want) {
			t.Errorf("desktop module missing %q", want)
		}
	}
	// The header rides the one shared headers builder (csrf), not a
	// second fetch site: one transport, one place the claim attaches.
	// Accept the minified and raw spellings.
	if !strings.Contains(src, "headers['X-Gofastr-Window']=windowID()") &&
		!strings.Contains(src, "headers['X-Gofastr-Window'] = windowID()") {
		t.Error("the window header must be set inside csrf(headers), the one shared headers builder")
	}
	// A marker with no window field falls back to "main".
	if !strings.Contains(src, "||'main'") && !strings.Contains(src, "|| 'main'") {
		t.Error("windowID must fall back to 'main' when the marker carries no window field")
	}
}

// TestRuntimeModule_DesktopSetPath pins the navigation report: the
// module listens for the router's gofastr:navigate event and sends
// window.setPath through the shared transport, once at load for the
// entry path. The report is a claim: the server validates it and keeps
// only the main window's.
func TestRuntimeModule_DesktopSetPath(t *testing.T) {
	src, ok := Module("desktop")
	if !ok {
		t.Fatal("desktop module not embedded")
	}
	for _, want := range []string{
		"gofastr:navigate",  // the router's post-swap event
		"setPath",           // the bridge method the report rides
		"location.pathname", // the at-load report of the entry path
		"e.detail",          // the event's path wins over location
		".path",             // (detail.path, minifier-safe spelling)
	} {
		if !strings.Contains(src, want) {
			t.Errorf("desktop module missing %q", want)
		}
	}
	// The report is fire-and-forget: a rejected setPath (remembering
	// off, an app that never Runs) must not surface as an error.
	if !strings.Contains(src, ".catch(()=>{})") && !strings.Contains(src, ".catch(() => {})") {
		t.Error("setPath report must swallow its rejection")
	}
	// A path not starting with "/" is never sent (the server would
	// refuse it anyway; the client check keeps broken pages quiet).
	if !strings.Contains(src, "charAt(0)==='/'") && !strings.Contains(src, "charAt(0) === '/'") {
		t.Error("setPath report must check the leading slash before sending")
	}
}
