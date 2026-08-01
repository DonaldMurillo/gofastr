package pluginhost

import (
	"strings"
	"testing"
)

// Manifest.Entry is the URL the broker loads into the plugin iframe, and it
// arrives with the third-party plugin — attacker-influenced by construction.
// It was unvalidated: an absolute "https://evil.example/x.html" or a
// protocol-relative "//evil.example/x.html" made the frame a document the host
// origin no longer controls, and the framed-CSP sandbox header that carries
// the opaque-origin guarantee is only emitted by AssetServer for assets it
// serves. A cross-origin Entry gets no such header.
func TestManifestRejectsOffOriginEntry(t *testing.T) {
	bad := []struct {
		entry string
		why   string
	}{
		{"https://evil.example/x.html", "absolute https URL"},
		{"http://evil.example/x.html", "absolute http URL"},
		{"//evil.example/x.html", "protocol-relative URL"},
		{"javascript:alert(1)", "javascript: scheme"},
		{"data:text/html,<script>alert(1)</script>", "data: URL"},
		{"\\\\evil.example\\x.html", "backslash-authority form"},
		{"/\\evil.example/x.html", "slash-backslash authority form"},
		{"relative/path.html", "relative path (resolves against the mounting page)"},
		{"/plugin/x.html\nX-Injected: 1", "control character"},
	}
	for _, tc := range bad {
		m := Manifest{Entry: tc.entry, Schema: "v1"}
		if err := m.Validate(); err == nil {
			t.Errorf("SECURITY: [isolation] Validate accepted %s: %q", tc.why, tc.entry)
		}
	}
}

// The legitimate shape must keep working, or plugins can't ship.
func TestManifestAcceptsSameOriginAbsolutePath(t *testing.T) {
	for _, entry := range []string{
		"/__gofastr/plugin/wysiwyg/editor.html",
		"/__gofastr/plugin/wysiwyg/editor.html?v=2",
		"/plugins/x/index.html",
	} {
		m := Manifest{Entry: entry, Schema: "v1"}
		if err := m.Validate(); err != nil {
			t.Errorf("Validate rejected a legitimate entry %q: %v", entry, err)
		}
	}
}

// The JS broker is the actual sink — it sets the iframe src. A Go-side
// Validate a plugin author might skip cannot be the only check, exactly as
// the sandbox invariant is dual-enforced.
func TestBrokerJSValidatesEntry(t *testing.T) {
	js := string(brokerJSBytes)
	if !strings.Contains(js, "safeEntry") {
		t.Fatal("pluginhost.js does not validate manifest.entry before setting iframe src")
	}
	// The frame src must be built from the validated value, never the raw
	// manifest field.
	for line := range strings.SplitSeq(js, "\n") {
		if strings.Contains(line, `setAttribute("src"`) && !strings.Contains(line, "entry") {
			t.Errorf("iframe src set from something other than the validated entry: %q", strings.TrimSpace(line))
		}
	}
}

// The Go sandbox sanitizer became an allow-list in v0.45.0; the JS one — the
// authoritative sink — was still a one-token deny-list, so
// allow-popups-to-escape-sandbox, allow-top-navigation and allow-downloads all
// passed through it. Two sanitizers with different verdicts is one sanitizer.
func TestBrokerJSSandboxIsAllowList(t *testing.T) {
	js := string(brokerJSBytes)
	if strings.Contains(js, "SAME_ORIGIN_COLLAPSING") {
		t.Error("pluginhost.js still uses the same-origin-only deny-list; it must be an allow-list")
	}
	if !strings.Contains(js, "ALLOWED_SANDBOX") {
		t.Fatal("pluginhost.js has no sandbox allow-list")
	}
	// Every token the Go allow-list grants must be grantable in JS too, or
	// the two sanitizers disagree about what a legitimate plugin may do.
	for tok := range allowedSandboxTokens {
		if !strings.Contains(js, `"`+tok+`"`) {
			t.Errorf("JS allow-list is missing %q, which Go grants", tok)
		}
	}
	// And the escape tokens must appear nowhere.
	for _, tok := range []string{
		"allow-same-origin",
		"allow-popups-to-escape-sandbox",
		"allow-top-navigation",
		"allow-top-navigation-by-user-activation",
		"allow-downloads",
	} {
		if strings.Contains(js, `"`+tok+`": true`) {
			t.Errorf("SECURITY: [isolation] JS allow-list grants escape token %q", tok)
		}
	}
}
